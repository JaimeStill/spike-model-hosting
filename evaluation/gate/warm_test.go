package gate_test

import (
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
)

// commands returns the router's load and unload requests, in order.
func (f *fakeRouter) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		if strings.HasPrefix(r, "load ") || strings.HasPrefix(r, "unload ") {
			out = append(out, r)
		}
	}
	return out
}

// reloadOf is set A's reload: every model unloaded, then every model loaded, in the set's order.
func reloadOf(models []string) []string {
	var out []string
	for _, verb := range []string{"unload", "load"} {
		for _, m := range models {
			out = append(out, verb+" "+m)
		}
	}
	return out
}

// TestRunReloads shows a model run and a set run each start by reloading the set over the
// router's API, after admission and before any request a stage sends, and record the idle pool
// and the warmed peak, each with system RAM beside it.
func TestRunReloads(t *testing.T) {
	for name, model := range map[string]string{"model run": gptOSS, "set run": ""} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, model)
			h.footprint.free = []int{3509, 3100}
			rep := h.run(t, "", nil)
			if got, want := h.router.commands(), reloadOf(h.runner.Set.Models); !slices.Equal(got, want) {
				t.Fatalf("commands %q; want %q", got, want)
			}
			// No model request came before the reload's last load.
			h.router.mu.Lock()
			last := slices.Index(h.router.requests, "load "+embedding)
			for _, r := range h.router.requests[:last] {
				if strings.HasPrefix(r, "chat") || strings.HasPrefix(r, "capacity") || r == "embeddings" || r == "messages" {
					t.Errorf("%q came before the reload finished", r)
				}
			}
			h.router.mu.Unlock()
			w := rep.Warm
			if w == nil || w.Reused || len(w.Reload) != 8 || w.Idle == nil || w.Peak == nil {
				t.Fatalf("warm %+v; want a reload of 8 steps with both readings", w)
			}
			if w.Idle.FreeMiB() != 3509 || w.Peak.FreeMiB() != 3100 {
				t.Errorf("idle %d, peak %d MiB free; want 3509 and 3100", w.Idle.FreeMiB(), w.Peak.FreeMiB())
			}
			for _, p := range []*gate.Pool{w.Idle, w.Peak} {
				if p.RAM == nil || p.RAM.UsedMiB == 0 || p.RAM.AvailableMiB == 0 || len(p.RAM.RSSMiB) != 4 {
					t.Errorf("reading %+v; want system RAM with each model's RSS", p)
				}
			}
			c := check(t, rep, gate.StageFitness, "", "warmed pool free")
			if !c.Passed || *c.Measured != 3100 || c.Values["idle_pool_free_mib"] != 3509 || c.Values["warm_growth_mib"] != 409 ||
				c.Values["ram_available_mib"] != 28000+3100 || c.Values["rss_mib "+gemma26B] != rssMiB {
				t.Errorf("warmed pool free %+v; want 3100 judged, the idle 3509, the growth and RAM beside it", c)
			}
			if !strings.Contains(c.Detail, "system RAM") || !strings.Contains(c.Detail, "idle after the reload, 3509 MiB free") {
				t.Errorf("warmed pool free's detail %q; want system RAM and the idle reading", c.Detail)
			}
			var sum strings.Builder
			if err := gate.WriteSummary(&sum, rep); err != nil {
				t.Fatal(err)
			}
			if want := "idle 3509 MiB free (system RAM 96491 MiB used, 31509 available), warmed peak 3100 MiB free (system RAM 96900 MiB used, 31100 available)"; !strings.Contains(sum.String(), want) {
				t.Errorf("summary lacks %q:\n%s", want, sum.String())
			}
		})
	}
}

// TestRunResumeDoesntReload shows a run resumed from a later stage on the same run's report
// reloads nothing and keeps that run's warm-up, marked reused.
func TestRunResumeDoesntReload(t *testing.T) {
	h := newHarness(t, gptOSS)
	first := h.run(t, "", nil)
	before, peaks := len(h.router.commands()), len(h.uv.peakCalls(t))
	rep := h.run(t, gate.StageCapacity, first)
	if n := len(h.router.commands()); n != before {
		t.Errorf("the resumed run sent %d load or unload requests", n-before)
	}
	if rep.Warm == nil || !rep.Warm.Reused || rep.Warm.Peak == nil || rep.Warm.Peak.FreeMiB() != first.Warm.Peak.FreeMiB() {
		t.Errorf("resumed warm %+v; want the first run's, reused", rep.Warm)
	}
	if n := len(h.uv.peakCalls(t)); n != peaks {
		t.Errorf("the resumed run looked for peak media %d times", n-peaks)
	}
}

// TestWarmUpRequests shows the warm-up sends each of the set's models its high-water requests:
// an 8192-token prompt to each generative model, the vision model the largest image and the
// audio model the longest clip its tier lists send (found through uv into the run's warm/), and
// the embedding model an input filling its physical batch.
func TestWarmUpRequests(t *testing.T) {
	h := newHarness(t, embedding)
	rep := h.run(t, "", nil)
	var got []string
	for _, q := range rep.Warm.Requests {
		if q.Err != "" {
			t.Errorf("%s's %s failed: %s", q.Model, q.Name, q.Err)
		}
		got = append(got, q.Model+" "+q.Role+" "+q.Name)
	}
	want := []string{
		gptOSS + "  " + gate.WarmPrompt,
		gemma26B + "  " + gate.WarmPrompt, gemma26B + " vision " + gate.WarmImage,
		gemmaE4B + "  " + gate.WarmPrompt, gemmaE4B + " audio " + gate.WarmClip,
		embedding + " embedding " + gate.WarmInput,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("warm-up requests %q; want %q", got, want)
	}
	for _, m := range []string{gptOSS, gemma26B, gemmaE4B} {
		if n := h.router.longest[m]; n < 8192 {
			t.Errorf("%s's longest prompt %d tokens; want 8192", m, n)
		}
	}
	// set A's EmbeddingGemma 2 is c = 8192, less the room for its special tokens.
	if n := h.router.widest[embedding]; n != 8184 {
		t.Errorf("the embedding model's largest input %d tokens; want 8184", n)
	}
	image := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\npeak image"))
	if !slices.Contains(h.router.images, image) {
		t.Errorf("no request carried the peak image; got %.80q", h.router.images)
	}
	clip := base64.StdEncoding.EncodeToString([]byte("RIFF peak clip"))
	sent := false
	for _, b := range h.router.audio {
		parts := b["messages"].([]any)[0].(map[string]any)["content"].([]any)
		if a, ok := parts[1].(map[string]any)["input_audio"].(map[string]any); ok && a["data"] == clip {
			sent = true
		}
	}
	if !sent {
		t.Error("no request carried the peak clip")
	}
	if d := rep.Warm.Requests[2].Detail; d != "vision-vstar 83, 3300x2400" {
		t.Errorf("the image's detail %q; want its tier, sample and size", d)
	}
	if d := rep.Warm.Requests[4].Detail; d != "asr-earnings22 4483338/86.wav, 29.5s" {
		t.Errorf("the clip's detail %q; want its tier, utterance and length", d)
	}
	if calls := h.uv.peakCalls(t); len(calls) != 2 || !strings.Contains(calls[0], "vision_quality.py peak --tiers") ||
		!strings.Contains(calls[1], "asr_quality.py peak --tiers") {
		t.Errorf("peak calls %q; want vision_quality.py's then asr_quality.py's", calls)
	}
}

// TestWarmedPeakFloor shows the pool's floor is judged at the warmed peak, not the idle reading:
// a set idle above the floor fails once its warm-up leaves it under, in fitness, for a model run
// and a set run alike; a warm-up request that fails makes it a measurement that
// couldn't be taken.
func TestWarmedPeakFloor(t *testing.T) {
	for name, model := range map[string]string{"model run": gemma26B, "set run": ""} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, model)
			h.footprint.free = []int{3509, 2700}
			rep := h.run(t, "", nil)
			stage := gate.StageFitness
			c := check(t, rep, stage, "", "warmed pool free")
			if c.Passed || *c.Measured != 2700 || *c.Floor != 2816 || c.Values["idle_pool_free_mib"] != 3509 {
				t.Errorf("warmed pool free %+v; want a failure at 2700 against 2816, idle 3509", c)
			}
			if rep.Stage(stage).Status != gate.StatusFail {
				t.Errorf("%s %s; want failed", stage, rep.Stage(stage).Status)
			}
		})
	}
	t.Run("warm-up failed", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		h.uv.fail = "vision"
		h.runner.MTEB, h.runner.ASR, h.runner.Vision, h.runner.Peaks = h.uv.runner(), h.uv.runner(), h.uv.runner(), h.uv.runner()
		rep := h.run(t, "", nil)
		c := check(t, rep, gate.StageFitness, "", "warmed pool free")
		if c.Passed || c.Measured != nil || !strings.Contains(c.Detail, gemma26B+"'s largest image") {
			t.Errorf("warmed pool free %+v; want a measurement that couldn't be taken, naming the image", c)
		}
	})
}

// TestRunRefusesPartlyLoadedSet shows a run that would reload the set refuses one the router
// doesn't list wholly loaded, before it sends anything, since the reload never loads a model it
// didn't find loaded.
func TestRunRefusesPartlyLoadedSet(t *testing.T) {
	h := newHarness(t, gptOSS)
	h.router.status[embedding] = "unloaded"
	rep, err := h.runner.Run(t.Context(), "", nil)
	if !errors.Is(err, gate.ErrNotLoaded) || rep != nil || !strings.Contains(err.Error(), embedding) {
		t.Fatalf("Run = %v, %v; want ErrNotLoaded naming %s", rep, err, embedding)
	}
	if n := len(h.router.commands()); n != 0 {
		t.Errorf("%d load or unload requests", n)
	}
}

func TestParseFootprint(t *testing.T) {
	out := "amdgpu_top v0.11.5\n\n0000:c3:00.0 (Radeon 8060S Graphics), VRAM   496/  512 MiB, GTT 94971/98304 MiB\n" +
		"@@free\n" +
		"               total        used        free      shared  buff/cache   available\n" +
		"Mem:     134297174016 114884366336  2368667648    35495936 22860959744 19412807680\n" +
		"Swap:    167710834688       20480 167710814208\n" +
		"@@ps\n" +
		"142964 /opt/llama.cpp/current/llama-server --models-dir /home/jaime/models --port 8080\n" +
		"4832208 /opt/llama.cpp/b11734/llama-server --host 127.0.0.1 --alias ggml-org/gpt-oss-120b-GGUF:MXFP4 --ctx-size 131072\n" +
		"402504 /opt/llama.cpp/b11734/llama-server --embeddings --alias ggml-org/embeddinggemma-2-GGUF:Q8_0\n" +
		"9000 /usr/bin/bash\n"
	p, err := gate.ParseFootprint([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if p.UsedMiB != 94971 || p.RAM == nil || p.RAM.UsedMiB != 109562 || p.RAM.AvailableMiB != 18513 || p.RAM.TotalMiB != 128075 {
		t.Errorf("ParseFootprint = %+v, RAM %+v; want 94971 of 98304, RAM 109562 used, 18513 available", p, p.RAM)
	}
	if want := map[string]int{gptOSS: 4718, embedding: 393}; len(p.RAM.RSSMiB) != 2 || p.RAM.RSSMiB[gptOSS] != want[gptOSS] || p.RAM.RSSMiB[embedding] != want[embedding] {
		t.Errorf("RSS %v; want %v, the router itself left out", p.RAM.RSSMiB, want)
	}
	for _, bad := range []string{"GTT 1/2 MiB\n", "GTT 1/2 MiB\n@@free\nMem: x\n@@ps\n", "no device\n@@free\n@@ps\n"} {
		if _, err := gate.ParseFootprint([]byte(bad)); err == nil {
			t.Errorf("parsed %q", bad)
		}
	}
}

// TestWarmPromptFitsContext shows a generative model whose context is under the warm-up's
// 8192-token prompt, as a sweep step's can be, is sent the prompt cut to its context less the
// room for the template and the answer: the most it can be driven to.
func TestWarmPromptFitsContext(t *testing.T) {
	h := newHarness(t, embedding)
	h.runner.Profile.Model(gemmaE4B).C = 2048
	rep := h.run(t, "", nil)
	for _, q := range rep.Warm.Requests {
		if q.Err != "" {
			t.Errorf("%s's %s failed: %s", q.Model, q.Name, q.Err)
		}
	}
	if n := h.router.longest[gemmaE4B]; n < 2048-256 || n >= 2048 {
		t.Errorf("gemma-4-E4B's longest prompt %d tokens at c = 2048; want its c less 256, and the question", n)
	}
	if n := h.router.longest[gptOSS]; n < 8192 {
		t.Errorf("gpt-oss-120b's longest prompt %d tokens; want the full 8192", n)
	}
}
