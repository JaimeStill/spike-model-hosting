package gate_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// strixHalo is the profile the runs are judged by: set A's models, roles and budget.
const strixHalo = "../../devices/strix-halo/profile.toml"

const (
	gptOSS    = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
	gemma26B  = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	gemmaE4B  = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
	embedding = "ggml-org/embeddinggemma-2-GGUF:Q8_0"
)

// harness is one run's fakes and the runner over them.
type harness struct {
	router    *fakeRouter
	footprint *fakeFootprint
	uv        *fakeUV
	conform   *fakeConform
	guidellm  *fakeGuideLLM
	runner    *gate.Runner
}

// newHarness returns a runner for model in set A against a fake router with set A loaded, the
// test floors (testdata/floors.toml), a pool with 3333MiB free, as set A left it idle on b11669, no bars, a fake uv
// that answers each MTEB task, ASR set, vision tier, BFCL tier and τ2 domain with its recorded
// result, a fake conform run in which Pi fits and Claude Code and OpenCode drop at their first
// trial, and a fake GuideLLM that answers every cell with a recorded one; the architect isn't at
// hand to restart the router.
func newHarness(t *testing.T, model string) *harness {
	t.Helper()
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	set, err := p.Set("a")
	if err != nil {
		t.Fatal(err)
	}
	floors, err := gate.LoadFloors("testdata/floors.toml")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		router:    newFakeRouter(t, set.Models...),
		footprint: &fakeFootprint{free: []int{3333}},
	}
	h.uv = newFakeUV(t)
	h.conform = newFakeConform(gate.ConformK)
	h.guidellm = newFakeGuideLLM(t)
	tiers, err := filepath.Abs("testdata/tiers")
	if err != nil {
		t.Fatal(err)
	}
	uv := h.uv.runner()
	h.runner = &gate.Runner{
		Router: h.router.srv.URL, Profile: p, Set: set, Model: model, Floors: floors,
		Footprint: h.footprint, SampleEvery: 5 * time.Millisecond, CrossEvery: 5 * time.Millisecond,
		Media: gate.Media{PNG: []byte("png"), WAV: []byte("wav"), AudioSeconds: 6},
		MTEB:  uv, ASR: uv, Vision: uv, Agent: uv, Dir: t.TempDir(), Tiers: tiers,
		Conform: h.conform, Tau2: uv, Grid: h.guidellm.runner(), PollEvery: 2 * time.Millisecond,
	}
	return h
}

func (h *harness) run(t *testing.T, from string, prior *gate.Report) *gate.Report {
	t.Helper()
	rep, err := h.runner.Run(t.Context(), from, prior)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return rep
}

// statuses returns each stage's status, in the report's order.
func statuses(rep *gate.Report) []string {
	var out []string
	for _, s := range rep.Stages {
		out = append(out, s.Name+" "+s.Status)
	}
	return out
}

// check returns the stage's check named name for role, failing t when there is none.
func check(t *testing.T, rep *gate.Report, stage, role, name string) gate.Check {
	t.Helper()
	s := rep.Stage(stage)
	if s == nil {
		t.Fatalf("no %s stage in %v", stage, statuses(rep))
	}
	for _, c := range s.Checks {
		if c.Role == role && c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check %q for role %q in %+v", stage, name, role, s.Checks)
	return gate.Check{}
}

func equal(a, b []string) bool { return strings.Join(a, "|") == strings.Join(b, "|") }

// TestRunAgentPasses runs gpt-oss-120b, set A's agent, through every stage.
func TestRunAgentPasses(t *testing.T) {
	h := newHarness(t, gptOSS)
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q\n%+v", got, want, rep.Stages)
	}
	if !rep.Passed || rep.Reached != gate.StageDeep {
		t.Errorf("passed %v, reached %q; want passed, reached deep", rep.Passed, rep.Reached)
	}
	// Capacity put the profile's 4 slots in flight, with the set's embedding stream beside them.
	if c := check(t, rep, gate.StageCapacity, gate.RoleAgent, "no crossed nonce"); !strings.HasPrefix(c.Detail, "4 answers in flight") {
		t.Errorf("capacity's nonce check %q; want 4 answers in flight", c.Detail)
	}
	check(t, rep, gate.StageCapacity, gate.RoleAgent, "embedding tolerance")
}

// TestRunOtherRoles runs each other role's model of set A through the built stages: quality runs
// MTEB's tasks for the embedding role, the ASR sets for the audio role and the vision tiers for
// the vision role.
func TestRunOtherRoles(t *testing.T) {
	for model, role := range map[string]string{gemma26B: gate.RoleVision, gemmaE4B: gate.RoleAudio, embedding: gate.RoleEmbedding} {
		t.Run(role, func(t *testing.T) {
			h := newHarness(t, model)
			rep := h.run(t, "", nil)
			if !rep.Passed || !equal(rep.Roles, []string{role}) {
				t.Errorf("passed %v roles %q; want passed, %s\n%+v", rep.Passed, rep.Roles, role, rep.Stages)
			}
			var runs int
			switch role {
			case gate.RoleVision:
				runs = len(gate.VisionTiers)
			case gate.RoleEmbedding:
				runs = len(gate.EmbeddingTasks)
			case gate.RoleAudio:
				runs = len(gate.AudioSets)
			}
			if s := rep.Stage(gate.StageQuality).Status; s != gate.StatusPass || rep.Reached != gate.StageDeep {
				t.Errorf("quality %s, reached %s; want pass, deep", s, rep.Reached)
			}
			if n := len(h.uv.calls(t)); n != runs {
				t.Errorf("uv ran %d times; want %d", n, runs)
			}
		})
	}
}

// TestRunStopsAtFirstFailure shows a failed stage stops the run: every later stage is reported
// not run, naming the stage that failed, and sends nothing.
func TestRunStopsAtFirstFailure(t *testing.T) {
	t.Run("fitness", func(t *testing.T) {
		h := newHarness(t, gptOSS)
		h.router.decode = 15
		rep := h.run(t, "", nil)
		want := []string{"admission pass", "fitness fail", "capacity not run", "quality not run", "set not run", "deep not run"}
		if got := statuses(rep); !equal(got, want) {
			t.Fatalf("stages %q; want %q", got, want)
		}
		if r := rep.Stage(gate.StageCapacity).Reason; r != "fitness failed" {
			t.Errorf("capacity's reason %q; want fitness failed", r)
		}
		if rep.Passed || rep.Reached != gate.StageFitness {
			t.Errorf("passed %v, reached %q; want failed at fitness", rep.Passed, rep.Reached)
		}
		if n := h.router.count("capacity"); n != 0 {
			t.Errorf("capacity sent %d requests after fitness failed", n)
		}
	})
	t.Run("admission", func(t *testing.T) {
		h := newHarness(t, gptOSS)
		set := *h.runner.Set
		set.MemoryBudget.MeasuredMiB = 97000 // 1304MiB free, under the 2816 floor
		h.runner.Set = &set
		rep := h.run(t, "", nil)
		want := []string{"admission fail", "fitness not run", "capacity not run", "quality not run", "set not run", "deep not run"}
		if got := statuses(rep); !equal(got, want) {
			t.Fatalf("stages %q; want %q", got, want)
		}
		if c := check(t, rep, gate.StageAdmission, "", "memory budget"); c.Passed || *c.Measured != 1304 {
			t.Errorf("memory budget %+v; want a failure at 1304MiB free", c)
		}
		if n := h.router.count("chat") + h.router.count("messages") + h.router.count("embeddings"); n != 0 {
			t.Errorf("%d model requests after admission failed", n)
		}
	})
}

// TestRunRefusesUnloadedModel shows a model the router lists unloaded is refused before any
// stage, and nothing is sent to it.
func TestRunRefusesUnloadedModel(t *testing.T) {
	h := newHarness(t, gptOSS)
	h.router.status[gptOSS] = "unloaded"
	rep, err := h.runner.Run(t.Context(), "", nil)
	if !errors.Is(err, gate.ErrNotLoaded) || rep != nil || !strings.Contains(err.Error(), gptOSS+" as unloaded") {
		t.Fatalf("Run = %v, %v; want ErrNotLoaded naming the model unloaded", rep, err)
	}
	if n := h.router.count("props") + h.router.count("chat") + h.router.count("messages"); n != 0 {
		t.Errorf("%d requests past GET /models", n)
	}
}

// TestRunResumes runs a model that fails capacity, then resumes from capacity once the pool has
// room: admission and fitness are reused, not rerun.
func TestRunResumes(t *testing.T) {
	h := newHarness(t, gptOSS)
	h.footprint.free = []int{3333, 2500} // idle, then under the floor at the peak
	first := h.run(t, "", nil)
	if first.Stage(gate.StageCapacity).Status != gate.StatusFail {
		t.Fatalf("first run %q; want capacity failed", statuses(first))
	}
	if c := check(t, first, gate.StageCapacity, gate.RoleAgent, "peak pool free"); c.Passed || *c.Measured != 2500 {
		t.Errorf("peak pool free %+v; want a failure at 2500MiB", c)
	}

	h.footprint.free, h.footprint.n = []int{3333}, 0
	toolCalls := h.router.count("messages")
	rep := h.run(t, gate.StageCapacity, first)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
	if got := statuses(rep); !equal(got, want) || !rep.Passed || rep.From != gate.StageCapacity {
		t.Fatalf("resumed run %q, passed %v, from %q; want %q, passed, from capacity", got, rep.Passed, rep.From, want)
	}
	if !rep.Stage(gate.StageAdmission).Reused || !rep.Stage(gate.StageFitness).Reused || rep.Stage(gate.StageCapacity).Reused {
		t.Errorf("reused flags wrong: %+v", rep.Stages)
	}
	if h.router.count("messages") != toolCalls {
		t.Error("the resumed run reran fitness's tool call")
	}

	// A run whose fitness failed can't be resumed past it.
	failed := *first
	failed.Stages = []gate.StageResult{first.Stages[0], {Name: gate.StageFitness, Status: gate.StatusFail}}
	if _, err := h.runner.Run(t.Context(), gate.StageCapacity, &failed); err == nil || !strings.Contains(err.Error(), "fitness fail") {
		t.Errorf("resuming past a failed fitness: %v; want an error naming it", err)
	}
	if _, err := h.runner.Run(t.Context(), gate.StageCapacity, nil); err == nil {
		t.Error("resuming with no prior run ran")
	}
	other := *first
	other.Model = gemma26B
	if _, err := h.runner.Run(t.Context(), gate.StageCapacity, &other); err == nil {
		t.Error("resuming another model's run ran")
	}
}

// TestFloors judges each floor pass and fail from fake timings, or from a floor set past what
// the instant fake answers.
func TestFloors(t *testing.T) {
	tiny, huge := 1e-9, 1e12
	cases := []struct {
		name, model, stage, role, check string
		set                             func(h *harness)
		pass                            bool
	}{
		{"decode passes", gptOSS, gate.StageFitness, gate.RoleAgent, "decode", func(h *harness) { h.router.decode = 25 }, true},
		{"decode fails", gptOSS, gate.StageFitness, gate.RoleAgent, "decode", func(h *harness) { h.router.decode = 15 }, false},
		{"prefill passes", gptOSS, gate.StageFitness, gate.RoleAgent, "prefill 8k", func(h *harness) { h.router.prefill = 600 }, true},
		{"prefill fails", gptOSS, gate.StageFitness, gate.RoleAgent, "prefill 8k", func(h *harness) { h.router.prefill = 400 }, false},
		{"ttft passes", gptOSS, gate.StageFitness, gate.RoleAgent, "ttft 8k", func(*harness) {}, true},
		{"ttft fails", gptOSS, gate.StageFitness, gate.RoleAgent, "ttft 8k", func(h *harness) { h.runner.Floors.Agent.TTFT8kS = tiny }, false},
		{"4-stream decode passes", gptOSS, gate.StageCapacity, gate.RoleAgent, "4-stream decode", func(h *harness) { h.router.decode = 21 }, true},
		{"4-stream decode fails", gptOSS, gate.StageCapacity, gate.RoleAgent, "4-stream decode", func(h *harness) {
			h.router.decode = 21
			h.runner.Floors.Agent.Stream4DecodeTokS = 22
		}, false},
		{"image answer passes", gemma26B, gate.StageFitness, gate.RoleVision, "image answer", func(*harness) {}, true},
		{"image answer fails", gemma26B, gate.StageFitness, gate.RoleVision, "image answer", func(h *harness) { h.runner.Floors.Vision.ImageAnswerS = tiny }, false},
		{"rtfx passes", gemmaE4B, gate.StageFitness, gate.RoleAudio, "rtfx", func(*harness) {}, true},
		{"rtfx fails", gemmaE4B, gate.StageFitness, gate.RoleAudio, "rtfx", func(h *harness) { h.runner.Floors.Audio.RTFx = huge }, false},
		{"4-stream rtfx passes", gemmaE4B, gate.StageCapacity, gate.RoleAudio, "4-stream rtfx", func(*harness) {}, true},
		{"4-stream rtfx fails", gemmaE4B, gate.StageCapacity, gate.RoleAudio, "4-stream rtfx", func(h *harness) { h.runner.Floors.Audio.Stream4RTFx = huge }, false},
		{"throughput passes", embedding, gate.StageFitness, gate.RoleEmbedding, "throughput", func(*harness) {}, true},
		{"throughput fails", embedding, gate.StageFitness, gate.RoleEmbedding, "throughput", func(h *harness) { h.runner.Floors.Embedding.ThroughputTokS = huge }, false},
		{"query passes", embedding, gate.StageFitness, gate.RoleEmbedding, "query 32", func(*harness) {}, true},
		{"query fails", embedding, gate.StageFitness, gate.RoleEmbedding, "query 32", func(h *harness) { h.runner.Floors.Embedding.Query32MS = tiny }, false},
		{"idle pool passes", gptOSS, gate.StageFitness, "", "idle pool free", func(*harness) {}, true},
		{"idle pool fails", gptOSS, gate.StageFitness, "", "idle pool free", func(h *harness) { h.footprint.free = []int{2700} }, false},
		{"peak pool fails", embedding, gate.StageCapacity, gate.RoleEmbedding, "peak pool free", func(h *harness) { h.footprint.free = []int{3333, 2700} }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.model)
			tc.set(h)
			rep := h.run(t, "", nil)
			c := check(t, rep, tc.stage, tc.role, tc.check)
			if c.Passed != tc.pass {
				t.Errorf("%s passed = %v; want %v (%+v)", tc.check, c.Passed, tc.pass, c)
			}
			if c.Measured == nil || c.Floor == nil || c.Op == "" {
				t.Errorf("%s records no measured value, floor or comparison: %+v", tc.check, c)
			}
			if st := rep.Stage(tc.stage).Status; (st == gate.StatusPass) != tc.pass {
				t.Errorf("%s stage %s; want passed %v", tc.stage, st, tc.pass)
			}
		})
	}
}

// TestTranscriptionRequest shows fitness's timed transcription and capacity's are one shape, the
// quality stage's ASR task's: the card's prompt, asr_quality.py's PROMPT, first and the clip after
// it, at temperature 0, with reasoning and the prompt cache off; capacity's carries its nonce
// request after the clip.
func TestTranscriptionRequest(t *testing.T) {
	src, err := os.ReadFile("../suites/asr_quality.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^PROMPT = "([^"]*)"$`).FindSubmatch(src)
	if m == nil {
		t.Fatal(`asr_quality.py holds no PROMPT = "..." line`)
	}
	prompt := string(m[1])

	h := newHarness(t, gemmaE4B)
	h.run(t, "", nil)
	// The set stage's crossing probe sends capacity's request too, a round at a time.
	if len(h.router.audio) < 1+4+1 {
		t.Fatalf("%d audio requests; want fitness's one, capacity's 4 and the set stage's probe", len(h.router.audio))
	}
	for i, body := range h.router.audio {
		kwargs, _ := body["chat_template_kwargs"].(map[string]any)
		if think, ok := kwargs["enable_thinking"].(bool); !ok || think {
			t.Errorf("request %d: chat_template_kwargs %v; want enable_thinking false", i, body["chat_template_kwargs"])
		}
		if body["temperature"] != 0.0 || body["cache_prompt"] != false {
			t.Errorf("request %d: temperature %v cache_prompt %v; want 0 and false", i, body["temperature"], body["cache_prompt"])
		}
		parts := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
		want := 2
		if i > 0 {
			want = 3
		}
		if len(parts) != want {
			t.Fatalf("request %d: %d content parts; want %d", i, len(parts), want)
		}
		first, second := parts[0].(map[string]any), parts[1].(map[string]any)
		if first["type"] != "text" || first["text"] != prompt || second["type"] != "input_audio" {
			t.Errorf("request %d: parts %v, %v; want the text %q, then the clip", i, first["type"], second["type"], prompt)
		}
		if i > 0 && !fakeNonce.MatchString(parts[2].(map[string]any)["text"].(string)) {
			t.Errorf("request %d: the part after the clip %v; want the nonce request", i, parts[2])
		}
	}
}

// TestAdmissionBuild shows admission fails a router below the profile's minimum build.
func TestAdmissionBuild(t *testing.T) {
	h := newHarness(t, gptOSS)
	h.router.build = "b11000-abc"
	rep := h.run(t, "", nil)
	if c := check(t, rep, gate.StageAdmission, "", "router build"); c.Passed || *c.Measured != 11000 || *c.Floor != 11452 {
		t.Errorf("router build %+v; want a failure, 11000 against 11452", c)
	}
}

// TestReport writes a run's report and reads it back with every field the report promises, and
// summarizes it.
func TestReport(t *testing.T) {
	h := newHarness(t, gptOSS)
	rep := h.run(t, "", nil)
	state := t.TempDir()
	dir := gate.RunDir(state, rep.Model, rep.Started)
	if err := gate.WriteReport(dir, rep); err != nil {
		t.Fatal(err)
	}
	if latest, err := gate.LatestRun(state, gptOSS); err != nil || latest != dir {
		t.Errorf("LatestRun = %q, %v; want %q", latest, err, dir)
	}
	if !strings.HasSuffix(dir, "-ggml-org_gpt-oss-120b-GGUF_MXFP4") || filepath.Dir(dir) != filepath.Join(state, "evaluate") {
		t.Errorf("run directory %q", dir)
	}
	got, err := gate.ReadReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Build != "b11669-620d2225c" || got.Set != "a" || got.Model != gptOSS || !equal(got.Roles, []string{gate.RoleAgent}) ||
		got.Reached != gate.StageDeep || !got.Passed || got.HostClass != "strix-halo" || got.Router != h.router.srv.URL {
		t.Errorf("report header %+v", got)
	}
	for _, s := range got.Stages {
		if s.WallS <= 0 || len(s.Checks) == 0 {
			t.Errorf("stage %s: wall %v s, %d checks; want both recorded", s.Name, s.WallS, len(s.Checks))
		}
	}
	d := check(t, got, gate.StageFitness, gate.RoleAgent, "decode")
	if *d.Measured != 50 || *d.Floor != 20 || d.Op != gate.AtLeast || d.Unit != "tok/s" {
		t.Errorf("decode %+v; want 50 tok/s against >= 20", d)
	}
	if l := check(t, got, gate.StageAdmission, "", "licence"); l.Detail != "Apache-2.0" || !l.Passed {
		t.Errorf("licence %+v", l)
	}
	// 5.1B active at 4.34 bits is 2.76675GB a token; 256GB/s over it, 92.53 tok/s.
	c := got.Ceiling
	if c == nil || c.DecodeTokS != 50 || math.Abs(c.BytesPerToken-2.76675e9) > 1 ||
		math.Abs(c.CeilingTokS-92.528) > 0.01 || math.Abs(c.Efficiency-0.54037) > 1e-4 || c.Source != "agent decode" {
		t.Errorf("ceiling %+v; want 50 of 92.53 tok/s from agent decode", c)
	}

	var sum bytes.Buffer
	if err := gate.WriteSummary(&sum, got); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"build b11669-620d2225c, set a, roles agent", "reached deep: passed",
		"ok   agent decode: 50 tok/s, floor >= 20", "ok   agent agent-tau2-airline: 0.5 pass^1, no floor", "ceiling: decode 50.0 tok/s of 92.5"} {
		if !strings.Contains(sum.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, sum.String())
		}
	}
}

// TestLoadFloors reads a floors file into each role's floors by its TOML key, loads the committed
// evaluation/floors.toml, and refuses a key the schema doesn't define or a floor missing.
func TestLoadFloors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "floors.toml")
	text := "[pool]\nfree_mib = 1\n[agent]\ndecode_tok_s = 2\nprefill_tok_s = 3\nttft_8k_s = 4\nstream4_decode_tok_s = 5\n" +
		"[vision]\nimage_answer_s = 6\n[audio]\nrtfx = 7\nstream4_rtfx = 8\n[embedding]\nthroughput_tok_s = 9\nquery32_ms = 10\n"
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := gate.LoadFloors(p)
	got := []float64{f.Pool.FreeMiB, f.Agent.DecodeTokS, f.Agent.PrefillTokS, f.Agent.TTFT8kS, f.Agent.Stream4DecodeTokS,
		f.Vision.ImageAnswerS, f.Audio.RTFx, f.Audio.Stream4RTFx, f.Embedding.ThroughputTokS, f.Embedding.Query32MS}
	if err != nil || !slices.Equal(got, []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
		t.Errorf("LoadFloors = %v, %v; want 1 to 10, each floor by its key", got, err)
	}
	if _, err := gate.LoadFloors("../floors.toml"); err != nil {
		t.Errorf("the committed floors: %v", err)
	}
	for name, text := range map[string]string{
		"unknown key": "[agent]\ncolour = 1\n",
		"missing":     "[pool]\nfree_mib = 3072\n",
	} {
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := gate.LoadFloors(p); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestParseRole(t *testing.T) {
	for in, want := range map[string]string{"agent": gate.RoleAgent, "chat": gate.RoleAgent, "vision": gate.RoleVision} {
		if got, err := gate.ParseRole(in); err != nil || got != want {
			t.Errorf("ParseRole(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := gate.ParseRole("reasoning"); err == nil {
		t.Error("ParseRole(reasoning) succeeded")
	}
}

func TestParseAmdgpuTop(t *testing.T) {
	out := "amdgpu_top v0.11.5\n\n0000:c3:00.0 (Radeon 8060S Graphics), VRAM   496/  512 MiB, GTT 94971/98304 MiB\n" +
		"    llama-server    (2595869), ctxs  1, VRAM    53 MiB ( 10%), GTT 65467 MiB ( 66%)\n"
	p, err := gate.ParseAmdgpuTop([]byte(out))
	if err != nil || p.UsedMiB != 94971 || p.TotalMiB != 98304 || p.FreeMiB() != 3333 {
		t.Errorf("ParseAmdgpuTop = %+v, %v; want 94971 of 98304", p, err)
	}
	if _, err := gate.ParseAmdgpuTop([]byte("no device")); err == nil {
		t.Error("parsed output without a GTT line")
	}
}

func TestWAVSeconds(t *testing.T) {
	// 16kHz mono 16-bit: 32000 bytes a second; 48000 bytes of data is 1.5s.
	var b bytes.Buffer
	b.WriteString("RIFF\x00\x00\x00\x00WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, []uint32{16})
	_ = binary.Write(&b, binary.LittleEndian, []uint16{1, 1})
	_ = binary.Write(&b, binary.LittleEndian, []uint32{16000, 32000})
	_ = binary.Write(&b, binary.LittleEndian, []uint16{2, 16})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, []uint32{48000})
	b.Write(make([]byte, 48000))
	if s, err := gate.WAVSeconds(b.Bytes()); err != nil || s != 1.5 {
		t.Errorf("WAVSeconds = %v, %v; want 1.5", s, err)
	}
	m, err := gate.LoadMedia("../..")
	if err != nil || m.AudioSeconds <= 0 || len(m.PNG) == 0 {
		t.Errorf("LoadMedia: %v seconds, %d PNG bytes, %v", m.AudioSeconds, len(m.PNG), err)
	}
}
