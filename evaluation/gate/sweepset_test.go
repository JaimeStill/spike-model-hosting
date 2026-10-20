package gate_test

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// setFootprint reads the pool by every model's served slots together: free gives the pool's
// free memory at their sum.
type setFootprint struct {
	router *fakeRouter
	free   func(served map[string]gate.Setting) int
}

func (f *setFootprint) Read(context.Context) (gate.Pool, error) {
	f.router.mu.Lock()
	served := make(map[string]gate.Setting, len(f.router.served))
	maps.Copy(served, f.router.served)
	f.router.mu.Unlock()
	free := f.free(served)
	ram := &gate.RAM{UsedMiB: 100000 - free, AvailableMiB: 28000 + free, TotalMiB: 128076}
	return gate.Pool{UsedMiB: 98304 - free, TotalMiB: 98304, RAM: ram}, nil
}

// totalSlots is the free memory a pool leaves when each slot costs 50MiB and 23 slots in all
// leave 2816MiB, the pool's floor: the sum of the set's served slots holds the floor up to 23.
func totalSlots(served map[string]gate.Setting) int {
	n := 0
	for _, s := range served {
		n += s.Slots
	}
	return 2816 + 50*(23-n)
}

// setSweepHarness is a set sweep of set A over the harness's fakes: its profile a copy of the
// test profile on the test's disk, which the sweep commits into, and the router's presets file
// the copy's render.
type setSweepHarness struct {
	*harness
	sweep   *gate.SetSweep
	presets *filePresets
	path    string
	dirs    map[string]string
}

func newSetSweepHarness(t *testing.T, free func(map[string]gate.Setting) int) *setSweepHarness {
	t.Helper()
	h := newHarness(t, "")
	src, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := h.runner.Profile.RenderPresets(&b); err != nil {
		t.Fatal(err)
	}
	ini := filepath.Join(t.TempDir(), "models.ini")
	h.router.usePresets(t, ini, b.Bytes())
	h.runner.Footprint = &setFootprint{router: h.router, free: free}
	presets := &filePresets{path: ini, committed: b.Bytes()}
	sh := &setSweepHarness{harness: h, presets: presets, path: path, dirs: map[string]string{}}
	sh.sweep = &gate.SetSweep{
		Runner: h.runner, Presets: presets, Targets: smallAgent(t), Profile: gate.FileProfile{Path: path},
		DirOf: func(model string) string {
			d := filepath.Join(t.TempDir(), gate.Slug(model))
			sh.dirs[model] = d
			return d
		},
	}
	return sh
}

// models returns the reports' models, in the order swept.
func models(reps []*gate.SweepReport) []string {
	var out []string
	for _, r := range reps {
		out = append(out, r.Model)
	}
	return out
}

// committed returns the profile on disk, and its render's SHA-256.
func (h *setSweepHarness) committed(t *testing.T) (*profile.Profile, string) {
	t.Helper()
	p, err := profile.Load(h.path)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := p.RenderPresets(&b); err != nil {
		t.Fatal(err)
	}
	return p, sha(b.Bytes())
}

// TestSetSweep shows a set sweep sweeps set A's models in role order, agent, vision, audio and
// embedding, each against the settings chosen before it: after each model's sweep its chosen
// slots and c are written into the profile with reasons that cite the sweep, the new render is
// installed and verified, and the next sweep restores to it and runs with the earlier models at
// their chosen settings. Each slot costs the pool 50MiB, so what the pool holds for a model
// depends on what was chosen before it: gpt-oss-120b, swept first beside 12 committed slots,
// holds 8; Gemma 4 26B-A4B, beside gpt-oss-120b's 8, holds 6, where at gpt-oss-120b's committed
// 4 it would have held 8.
func TestSetSweep(t *testing.T) {
	h := newSetSweepHarness(t, totalSlots)
	h.router.decodeAt = func(int) float64 { return 50 }
	reps, err := h.sweep.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{gptOSS, gemma26B, gemmaE4B, embedding}; !slices.Equal(models(reps), want) {
		t.Fatalf("swept %v; want role order %v", models(reps), want)
	}
	if ns(reps[0]) == nil || reps[0].Chosen != 8 || !slices.Equal(reps[0].Bounds, []string{gate.BoundPool}) {
		t.Errorf("gpt-oss-120b: steps %v, chosen %d by %v; want 8 by the pool", ns(reps[0]), reps[0].Chosen, reps[0].Bounds)
	}
	if want := []int{1, 2, 4, 8, 6}; !slices.Equal(ns(reps[1]), want) || reps[1].Chosen != 6 || !slices.Equal(reps[1].Bounds, []string{gate.BoundPool}) {
		t.Errorf("Gemma 4 26B-A4B: steps %v, chosen %d by %v; want steps %v, 6 by the pool", ns(reps[1]), reps[1].Chosen, reps[1].Bounds, want)
	}
	if reps[3].Chosen != 1 || !slices.Equal(reps[3].Bounds, []string{gate.BoundContext}) {
		t.Errorf("EmbeddingGemma 2: chosen %d by %v; want 1, its target filling its card", reps[3].Chosen, reps[3].Bounds)
	}

	p, sum := h.committed(t)
	chosen := map[string]gate.Setting{}
	for i, rep := range reps {
		var st *gate.SweepStep
		for j := range rep.Steps {
			if rep.Steps[j].N == rep.Chosen {
				st = &rep.Steps[j]
			}
		}
		if st == nil {
			t.Fatalf("%s: no step at its chosen %d", rep.Model, rep.Chosen)
		}
		want := gate.Setting{Slots: rep.Chosen, C: st.C}
		// The model's chosen setting is committed, with reasons that cite its sweep.
		m := p.Model(rep.Model)
		if (gate.Setting{Slots: m.Slots, C: m.C}) != want {
			t.Errorf("%s committed at %d slots and c = %d; want its chosen %+v", rep.Model, m.Slots, m.C, want)
		}
		for _, reason := range []string{m.SlotsReason, m.CReason} {
			if !strings.Contains(reason, "evaluate sweep") || !strings.Contains(reason, rep.Build) || !strings.Contains(reason, rep.Started.Format("2006-01-02")) {
				t.Errorf("%s's reason %q doesn't cite its sweep, build and date", rep.Model, reason)
			}
		}
		if !strings.Contains(m.SlotsReason, "Bound by "+strings.Join(rep.Bounds, " and ")) {
			t.Errorf("%s's slots reason %q doesn't name its bound %v", rep.Model, m.SlotsReason, rep.Bounds)
		}
		// Its new render was installed and verified, and the next sweep restored to it.
		if rep.Install == nil || !rep.Install.Verified || rep.Install.Installed != rep.Install.SHA256 {
			t.Errorf("%s's install %+v; want verified", rep.Model, rep.Install)
		} else if i+1 < len(reps) && reps[i+1].Restore.SHA256 != rep.Install.SHA256 {
			t.Errorf("%s's sweep restored %s; want %s, the render installed after %s", reps[i+1].Model, reps[i+1].Restore.SHA256, rep.Install.SHA256, rep.Model)
		}
		if rep.Install != nil && rep.Install.Served[rep.Model] != want {
			t.Errorf("%s served at %+v after its install; want %+v", rep.Model, rep.Install.Served[rep.Model], want)
		}
		// Each of its steps ran the models swept before it at their chosen settings, and the
		// models after it at their committed ones.
		for _, step := range rep.Steps {
			if step.Capped {
				continue
			}
			for _, id := range h.runner.Set.Models {
				if id == rep.Model {
					continue
				}
				w, ok := chosen[id]
				if !ok {
					w = gate.Setting{Slots: 4, C: h.runner.Profile.Model(id).C}
				}
				if step.Served[id] != w {
					t.Errorf("%s's step %d served %s at %+v; want %+v", rep.Model, step.N, id, step.Served[id], w)
				}
			}
		}
		chosen[rep.Model] = want
		if _, err := os.Stat(filepath.Join(h.dirs[rep.Model], gate.SweepFile)); err != nil {
			t.Errorf("%s's report: %v", rep.Model, err)
		}
	}
	if last := reps[len(reps)-1].Install; last == nil || last.SHA256 != sum {
		t.Errorf("the last install %+v; want the committed profile's render, sha256 %s", last, sum)
	}
	if b, _ := os.ReadFile(h.presets.path); sha(b) != sum {
		t.Errorf("the presets file is sha256 %s after the set sweep; want the committed profile's render, %s", sha(b), sum)
	}

	var out bytes.Buffer
	if err := gate.WriteSetSweepSummary(&out, "a", reps); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"set a swept, in role order:", "installed: the profile committed with " + gptOSS} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("summary lacks %q:\n%s", line, out.String())
		}
	}
}

// TestSetSweepFrom shows From starts the set sweep at its role's model: from audio, gemma-4-E4B
// and EmbeddingGemma 2 are swept, and the agent and vision models keep their committed settings
// and reasons.
func TestSetSweepFrom(t *testing.T) {
	h := newSetSweepHarness(t, totalSlots)
	h.sweep.From = gate.RoleAudio
	before, _ := h.committed(t)
	reps, err := h.sweep.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{gemmaE4B, embedding}; !slices.Equal(models(reps), want) {
		t.Fatalf("swept %v; want %v", models(reps), want)
	}
	after, _ := h.committed(t)
	for _, id := range []string{gptOSS, gemma26B} {
		if b, a := before.Model(id), after.Model(id); a.Slots != b.Slots || a.C != b.C || a.SlotsReason != b.SlotsReason || a.CReason != b.CReason {
			t.Errorf("%s changed: %+v; want it as committed", id, a)
		}
	}
}

// TestSetSweepStops shows a set sweep stops at a model none of whose steps holds: the pool falls
// under its floor whenever Gemma 4 26B-A4B runs at any setting but its committed one, so its
// sweep chooses nothing. gpt-oss-120b, swept before it, stays committed and installed; Gemma 4
// 26B-A4B stays at its committed setting, restored; the audio and embedding models aren't swept.
func TestSetSweepStops(t *testing.T) {
	h := newSetSweepHarness(t, func(served map[string]gate.Setting) int {
		if s := served[gemma26B]; s.Slots != 4 {
			return 2000
		}
		return totalSlots(served)
	})
	reps, err := h.sweep.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "no step of "+gemma26B+"'s sweep held") {
		t.Fatalf("Run = %v; want stopped at Gemma 4 26B-A4B", err)
	}
	if want := []string{gptOSS, gemma26B}; !slices.Equal(models(reps), want) {
		t.Fatalf("swept %v; want %v", models(reps), want)
	}
	if reps[1].Install != nil || reps[1].Restore == nil || !reps[1].Restore.Verified {
		t.Errorf("Gemma 4 26B-A4B: install %+v, restore %+v; want restored, not installed", reps[1].Install, reps[1].Restore)
	}
	p, sum := h.committed(t)
	if m := p.Model(gptOSS); m.Slots != reps[0].Chosen {
		t.Errorf("gpt-oss-120b committed at %d slots; want its chosen %d", m.Slots, reps[0].Chosen)
	}
	if m := p.Model(gemma26B); m.Slots != 4 || m.C != 32768 {
		t.Errorf("Gemma 4 26B-A4B committed at %d slots and c = %d; want its committed 4 and 32768", m.Slots, m.C)
	}
	if b, _ := os.ReadFile(h.presets.path); sha(b) != sum {
		t.Errorf("the presets file is sha256 %s; want the committed profile's render, %s", sha(b), sum)
	}
}
