package gate_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
)

// filePresets is the presets file on the test's disk: Install writes a temporary file beside it
// and renames it over it, as gate.SSHPresets does on the host. tamper, when set, rewrites what an
// install of the committed render writes, as a file left changed on the host would read.
type filePresets struct {
	mu        sync.Mutex
	path      string
	committed []byte
	tamper    bool
}

func (p *filePresets) Install(_ context.Context, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tamper && bytes.Equal(data, p.committed) {
		data = append(slices.Clone(data), []byte("; tampered\n")...)
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

func (p *filePresets) Sum(context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, err := os.ReadFile(p.path)
	if err != nil {
		return "", err
	}
	return sha(b), nil
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// sweepFootprint reads the pool by the swept model's served slots: free gives the pool's free
// memory at them (3333MiB when nil), and onRead, when set, sees each reading's slots.
type sweepFootprint struct {
	router *fakeRouter
	model  string
	free   func(slots int) int
	onRead func(slots int)
}

func (f *sweepFootprint) Read(context.Context) (gate.Pool, error) {
	f.router.mu.Lock()
	slots := f.router.served[f.model].Slots
	f.router.mu.Unlock()
	if f.onRead != nil {
		f.onRead(slots)
	}
	free := 3333
	if f.free != nil {
		free = f.free(slots)
	}
	ram := &gate.RAM{UsedMiB: 100000 - free, AvailableMiB: 28000 + free, TotalMiB: 128076}
	return gate.Pool{UsedMiB: 98304 - free, TotalMiB: 98304, RAM: ram}, nil
}

// sweepHarness is a sweep of one model of set A over the harness's fakes, the fake router's
// presets file a file of the test's holding the committed render.
type sweepHarness struct {
	*harness
	sweep     *gate.Sweep
	presets   *filePresets
	footprint *sweepFootprint
	committed []byte
}

// targets are the committed per-session targets, evaluation/targets.toml.
func targets(t *testing.T) gate.Targets {
	t.Helper()
	tg, err := gate.LoadTargets("../targets.toml")
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func newSweepHarness(t *testing.T, model string, tg gate.Targets) *sweepHarness {
	t.Helper()
	h := newHarness(t, model)
	var b bytes.Buffer
	if err := h.runner.Profile.RenderPresets(&b); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "models.ini")
	h.router.usePresets(t, path, b.Bytes())
	fp := &sweepFootprint{router: h.router, model: model}
	h.runner.Footprint = fp
	presets := &filePresets{path: path, committed: b.Bytes()}
	return &sweepHarness{
		harness: h, presets: presets, footprint: fp, committed: b.Bytes(),
		sweep: &gate.Sweep{Runner: h.runner, Presets: presets, Targets: tg},
	}
}

// ns returns the steps' N, in the order they ran.
func ns(rep *gate.SweepReport) []int {
	var out []int
	for _, st := range rep.Steps {
		out = append(out, st.N)
	}
	return out
}

// checkRestored fails t unless the presets file holds the committed render again, the report
// says the restore verified it, and every model of the set is loaded and serves at its committed
// setting.
func (h *sweepHarness) checkRestored(t *testing.T, rep *gate.SweepReport) {
	t.Helper()
	b, err := os.ReadFile(h.presets.path)
	if err != nil {
		t.Fatal(err)
	}
	if sha(b) != sha(h.committed) {
		t.Errorf("the presets file after the sweep is sha256 %s, not the committed render's %s", sha(b), sha(h.committed))
	}
	if rep.Restore == nil || !rep.Restore.Verified || rep.Restore.SHA256 != sha(h.committed) || rep.Restore.Installed != sha(h.committed) {
		t.Errorf("restore %+v; want verified, the committed render's sha256 %s installed", rep.Restore, sha(h.committed))
	}
	h.router.mu.Lock()
	defer h.router.mu.Unlock()
	for _, id := range h.runner.Set.Models {
		m := h.runner.Profile.Model(id)
		if got, want := h.router.served[id], (gate.Setting{Slots: m.Slots, C: m.C}); got != want || h.router.status[id] != "loaded" {
			t.Errorf("%s is %s at %+v after the sweep; want loaded at its committed %+v", id, h.router.status[id], got, want)
		}
	}
}

// TestSweepSteps shows a sweep's steps: N doubles through 1, 2, 4, 8 and 16 until a step fails,
// here 16, whose 16 sessions of the agent's 8704-token target need more than gpt-oss-120b's
// 131072-token card context, so c is capped there and the step fails on context with no request;
// then one bisection, 12. Each step's c is N times the target, rounded up to a multiple of 256;
// each is rendered from the profile, installed and reloaded, the swept model loaded again at it
// while the set's other models stay loaded at their own settings; and the sweep ends with the
// committed render restored, checked by its SHA-256.
func TestSweepSteps(t *testing.T) {
	h := newSweepHarness(t, gptOSS, targets(t))
	rep, err := h.sweep.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []int{1, 2, 4, 8, 16, 12}; !slices.Equal(ns(rep), want) {
		t.Fatalf("steps %v; want %v", ns(rep), want)
	}
	if rep.Target != 8704 || rep.TargetRole != gate.RoleAgent {
		t.Errorf("target %d (%s); want 8704, the agent's 8192 in and 512 out", rep.Target, rep.TargetRole)
	}
	wantC := map[int]int{1: 8704, 2: 17408, 4: 34816, 8: 69632, 16: 131072, 12: 104448}
	for _, st := range rep.Steps {
		if st.C != wantC[st.N] {
			t.Errorf("step %d: c = %d; want %d", st.N, st.C, wantC[st.N])
		}
		if st.C%256 != 0 {
			t.Errorf("step %d: c = %d, not a multiple of 256", st.N, st.C)
		}
		switch st.N {
		case 16:
			if !st.Capped || st.Held || !slices.Equal(st.Failed, []string{gate.BoundContext}) || st.Served != nil || len(st.Checks) != 0 {
				t.Errorf("step 16 %+v; want capped at the card's context, failed on context, no request sent", st)
			}
			continue
		case 12:
			if !st.Bisection {
				t.Errorf("step 12 isn't marked the bisection")
			}
		}
		if !st.Held || st.Capped {
			t.Errorf("step %d: held %v, failed %v; want held", st.N, st.Held, st.Failed)
		}
		for _, id := range h.runner.Set.Models {
			m := h.runner.Profile.Model(id)
			want := gate.Setting{Slots: m.Slots, C: m.C}
			if id == gptOSS {
				want = gate.Setting{Slots: st.N, C: st.C}
			}
			if st.Served[id] != want {
				t.Errorf("step %d: %s served at %+v; want %+v", st.N, id, st.Served[id], want)
			}
		}
		if st.Warm == nil || st.Warm.Peak == nil || st.Warm.Peak.RAM == nil {
			t.Errorf("step %d records no warmed peak with system RAM: %+v", st.N, st.Warm)
		}
		if len(st.Rates) != 1 || !st.Rates[0].Measured || st.Rates[0].Throughput != 50*float64(st.N) || st.Rates[0].TTFTMedianS != 0.12 || st.Rates[0].TTFTP95S != 0.12 {
			t.Errorf("step %d rates %+v; want the agent's %d streams' decode summed, %v tok/s, TTFT 0.12 s", st.N, st.Rates, st.N, 50*st.N)
		}
		decode := check(t, &gate.Report{Stages: []gate.StageResult{{Name: "x", Checks: st.Checks}}}, "x", gate.RoleAgent, "decode at N")
		if decode.Values["slots"] != float64(st.N) || decode.Values[fmt.Sprintf("stream%d", st.N)] == 0 {
			t.Errorf("step %d: decode at N %+v; want every one of its %d streams' rates", st.N, decode, st.N)
		}
	}
	if rep.Held != 12 || !slices.Equal(rep.HeldBy, []string{gate.BoundContext}) || rep.Knee != 0 || rep.Chosen != 12 ||
		!slices.Equal(rep.Bounds, []string{gate.BoundContext}) {
		t.Errorf("held %d by %v, knee %d, chosen %d by %v; want 12 held, bound by context, no knee", rep.Held, rep.HeldBy, rep.Knee, rep.Chosen, rep.Bounds)
	}
	// Only the swept model's section changed, so the router's reloads unloaded only it.
	for _, req := range h.router.requests {
		if m, ok := strings.CutPrefix(req, "reread-unload "); ok && m != gptOSS {
			t.Errorf("a reload unloaded %s; want only the swept %s", m, gptOSS)
		}
	}
	// The warm-up's peak media are found once, for every step.
	if calls := h.uv.peakCalls(t); len(calls) != 2 {
		t.Errorf("%d peak calls over the sweep; want 2, the vision and audio roles' once each", len(calls))
	}
	h.checkRestored(t, rep)

	var sum bytes.Buffer
	if err := gate.WriteSweepSummary(&sum, rep); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"held: N = 12 (the next step failed context); knee: none; chosen: N = 12, bound by context", "restored: the committed render, sha256 " + sha(h.committed)} {
		if !strings.Contains(sum.String(), line) {
			t.Errorf("summary lacks %q:\n%s", line, sum.String())
		}
	}
}

// TestSweepAdjacentNoBisection shows the bisection is skipped when the last step that held and
// the first that failed are adjacent: EmbeddingGemma 2's 8192-token target fills its 8192-token
// card at 1, so 2 is capped and fails on context, and 1 is chosen.
func TestSweepAdjacentNoBisection(t *testing.T) {
	h := newSweepHarness(t, embedding, targets(t))
	rep, err := h.sweep.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []int{1, 2}; !slices.Equal(ns(rep), want) {
		t.Fatalf("steps %v; want %v, no bisection between adjacent steps", ns(rep), want)
	}
	if rep.Steps[0].C != 8192 || !rep.Steps[0].Held || !rep.Steps[1].Capped || rep.Steps[1].C != 8192 {
		t.Errorf("steps %+v; want 1 held at c = 8192, and 2 capped at the card's 8192", rep.Steps)
	}
	if rep.Chosen != 1 || !slices.Equal(rep.Bounds, []string{gate.BoundContext}) {
		t.Errorf("chosen %d by %v; want 1 by context", rep.Chosen, rep.Bounds)
	}
	if r := rep.Steps[0].Rates; len(r) != 1 || r[0].Role != gate.RoleEmbedding || !r[0].Measured || r[0].Throughput <= 0 || r[0].TTFTMedianS <= 0 {
		t.Errorf("rates %+v; want the embedding queries' tokens a second and wall times", r)
	}
	h.checkRestored(t, rep)
}

// smallAgent is the committed targets with a 4608-token agent target, so 16 agent sessions fit
// gpt-oss-120b's card and every step of the doubling runs.
func smallAgent(t *testing.T) gate.Targets {
	tg := targets(t)
	tg.Agent = gate.Target{In: 4096, Out: 512, Source: "test"}
	return tg
}

// TestSweepFloorBound shows a step fails on its floor at N when one stream decodes under the
// per-stream floor, and the sweep holds the step below: at 8 one of the streams decodes at
// 5 tok/s, under the 10 tok/s floor, so 8 fails, 6 (the bisection) holds and is chosen, bound by
// the floor; throughput still gains, so there is no knee.
func TestSweepFloorBound(t *testing.T) {
	h := newSweepHarness(t, gptOSS, smallAgent(t))
	h.router.decodeAt = func(int) float64 { return 50 }
	h.router.slowAt = map[int]float64{8: 5}
	rep, err := h.sweep.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []int{1, 2, 4, 8, 6}; !slices.Equal(ns(rep), want) {
		t.Fatalf("steps %v; want %v", ns(rep), want)
	}
	if st := rep.Steps[3]; st.Held || !slices.Equal(st.Failed, []string{gate.BoundFloor}) {
		t.Errorf("step 8 held %v, failed %v; want failed on the floor alone", st.Held, st.Failed)
	}
	if rep.Held != 6 || !slices.Equal(rep.HeldBy, []string{gate.BoundFloor}) || rep.Knee != 0 || rep.Chosen != 6 || !slices.Equal(rep.Bounds, []string{gate.BoundFloor}) {
		t.Errorf("held %d by %v, knee %d, chosen %d by %v; want 6 by the floor, no knee", rep.Held, rep.HeldBy, rep.Knee, rep.Chosen, rep.Bounds)
	}
	h.checkRestored(t, rep)
}

// TestSweepPoolBound shows a step fails on the pool's floor when the set's pool at the warmed
// peak, with the model at N, leaves less than the floor free: from 8 slots 2000MiB, under 2816.
func TestSweepPoolBound(t *testing.T) {
	h := newSweepHarness(t, gptOSS, smallAgent(t))
	h.footprint.free = func(slots int) int {
		if slots >= 8 {
			return 2000
		}
		return 3333
	}
	rep, err := h.sweep.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []int{1, 2, 4, 8, 6}; !slices.Equal(ns(rep), want) {
		t.Fatalf("steps %v; want %v", ns(rep), want)
	}
	if st := rep.Steps[3]; st.Held || !slices.Equal(st.Failed, []string{gate.BoundPool}) {
		t.Errorf("step 8 held %v, failed %v; want failed on the pool alone", st.Held, st.Failed)
	}
	if rep.Held != 6 || rep.Chosen != 6 || !slices.Equal(rep.Bounds, []string{gate.BoundPool}) {
		t.Errorf("held %d, chosen %d by %v; want 6 by the pool", rep.Held, rep.Chosen, rep.Bounds)
	}
	h.checkRestored(t, rep)
}

// TestSweepContextBound shows a step fails on context when its requests run out of context, the
// context held check naming them: from 4 slots each of gpt-oss-120b's agentic requests is
// refused as llama.cpp refuses a prompt over its slot's context, so 4 fails and 3, the
// bisection, holds. The requests that ran out have no rate to judge, so the step names the
// context alone.
func TestSweepContextBound(t *testing.T) {
	h := newSweepHarness(t, gptOSS, smallAgent(t))
	h.router.exhaustAt = func(m string, slots int) bool { return m == gptOSS && slots >= 4 }
	rep, err := h.sweep.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []int{1, 2, 4, 3}; !slices.Equal(ns(rep), want) {
		t.Fatalf("steps %v; want %v", ns(rep), want)
	}
	if st := rep.Steps[2]; st.Held || !slices.Equal(st.Failed, []string{gate.BoundContext}) {
		t.Errorf("step 4 held %v, failed %v; want failed on context", st.Held, st.Failed)
	}
	if rep.Held != 3 || rep.Chosen != 3 || !slices.Equal(rep.Bounds, []string{gate.BoundContext}) {
		t.Errorf("held %d, chosen %d by %v; want 3 by context", rep.Held, rep.Chosen, rep.Bounds)
	}
	h.checkRestored(t, rep)
}

// TestSweepKnee shows the knee and the smaller of the two: with every step holding, total
// throughput (the streams' decode summed) gains under 10% from 4 to 8, so the knee is 4 and the
// chosen N is 4, bound by the knee, under the 16 that held; and where the knee and the last step
// that held fall together, both bounds are named.
func TestSweepKnee(t *testing.T) {
	decode := map[int]float64{1: 50, 2: 48, 3: 46, 4: 45, 6: 32, 8: 24, 16: 12}
	t.Run("under held", func(t *testing.T) {
		h := newSweepHarness(t, gptOSS, smallAgent(t))
		h.router.decodeAt = func(slots int) float64 { return decode[slots] }
		rep, err := h.sweep.Run(t.Context())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if want := []int{1, 2, 4, 8, 16}; !slices.Equal(ns(rep), want) {
			t.Fatalf("steps %v; want %v, every step held so no bisection", ns(rep), want)
		}
		if rep.Held != 16 || len(rep.HeldBy) != 0 || rep.Knee != 4 || rep.Chosen != 4 || !slices.Equal(rep.Bounds, []string{gate.BoundKnee}) {
			t.Errorf("held %d by %v, knee %d, chosen %d by %v; want 16 held, knee 4, 4 chosen by the knee", rep.Held, rep.HeldBy, rep.Knee, rep.Chosen, rep.Bounds)
		}
		var sum bytes.Buffer
		if err := gate.WriteSweepSummary(&sum, rep); err != nil {
			t.Fatal(err)
		}
		if line := "held: N = 16 (the sweep's last step held); knee: 4; chosen: N = 4, bound by knee"; !strings.Contains(sum.String(), line) {
			t.Errorf("summary lacks %q:\n%s", line, sum.String())
		}
		h.checkRestored(t, rep)
	})
	t.Run("at held", func(t *testing.T) {
		h := newSweepHarness(t, gptOSS, smallAgent(t))
		h.router.decodeAt = func(slots int) float64 { return decode[slots] }
		h.footprint.free = func(slots int) int {
			if slots >= 6 {
				return 2000
			}
			return 3333
		}
		rep, err := h.sweep.Run(t.Context())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if want := []int{1, 2, 4, 8, 6}; !slices.Equal(ns(rep), want) {
			t.Fatalf("steps %v; want %v", ns(rep), want)
		}
		if rep.Held != 4 || rep.Knee != 4 || rep.Chosen != 4 || !slices.Equal(rep.Bounds, []string{gate.BoundKnee, gate.BoundPool}) {
			t.Errorf("held %d, knee %d, chosen %d by %v; want 4, bound by the knee and the pool", rep.Held, rep.Knee, rep.Chosen, rep.Bounds)
		}
	})
	t.Run("over held", func(t *testing.T) {
		h := newSweepHarness(t, gptOSS, smallAgent(t))
		h.router.decodeAt = func(slots int) float64 { return decode[slots] }
		h.footprint.free = func(slots int) int {
			if slots >= 3 {
				return 2000
			}
			return 3333
		}
		rep, err := h.sweep.Run(t.Context())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if rep.Held != 2 || rep.Chosen != 2 || !slices.Equal(rep.Bounds, []string{gate.BoundPool}) {
			t.Errorf("held %d, knee %d, chosen %d by %v; want 2, under the knee, bound by the pool", rep.Held, rep.Knee, rep.Chosen, rep.Bounds)
		}
	})
}

// TestSweepRestoreOnFailure shows a sweep that stops on an error restores the committed render
// and verifies it: the router's third reload fails, mid-sweep.
func TestSweepRestoreOnFailure(t *testing.T) {
	h := newSweepHarness(t, gptOSS, smallAgent(t))
	h.router.failReload = 3
	rep, err := h.sweep.Run(t.Context())
	if err == nil || errors.Is(err, gate.ErrRestore) || !strings.Contains(err.Error(), "reload 3 fails") {
		t.Fatalf("Run = %v; want the failed reload's error, the restore passed", err)
	}
	if want := []int{1, 2, 4}; !slices.Equal(ns(rep), want) || rep.Err == "" || rep.Chosen != 0 {
		t.Errorf("steps %v, err %q, chosen %d; want stopped at 4, unchosen", ns(rep), rep.Err, rep.Chosen)
	}
	h.checkRestored(t, rep)
}

// TestSweepRestoreOnInterrupt shows an interrupted sweep, its context cancelled mid-step, still
// restores the committed render and verifies it, on a context of its own.
func TestSweepRestoreOnInterrupt(t *testing.T) {
	h := newSweepHarness(t, gptOSS, smallAgent(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.footprint.onRead = func(slots int) {
		if slots == 2 {
			cancel()
		}
	}
	rep, err := h.sweep.Run(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, gate.ErrRestore) {
		t.Fatalf("Run = %v; want cancelled, the restore passed", err)
	}
	if got := ns(rep); len(got) != 2 || got[1] != 2 {
		t.Errorf("steps %v; want stopped at 2", got)
	}
	h.checkRestored(t, rep)
}

// TestSweepRestoreSHAMismatch shows a restore whose installed file's SHA-256 isn't the committed
// render's fails loudly: the run's error wraps ErrRestore, and the report and summary say so.
func TestSweepRestoreSHAMismatch(t *testing.T) {
	h := newSweepHarness(t, embedding, targets(t))
	h.presets.tamper = true
	rep, err := h.sweep.Run(t.Context())
	if !errors.Is(err, gate.ErrRestore) || !strings.Contains(err.Error(), "not the committed render's") {
		t.Fatalf("Run = %v; want a failed restore naming the SHA-256 mismatch", err)
	}
	if rep.Restore == nil || rep.Restore.Verified || rep.Restore.Installed == rep.Restore.SHA256 {
		t.Errorf("restore %+v; want unverified, the installed file another", rep.Restore)
	}
	var sum bytes.Buffer
	if err := gate.WriteSweepSummary(&sum, rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sum.String(), "RESTORE FAILED: ") {
		t.Errorf("summary doesn't say the restore failed:\n%s", sum.String())
	}
}

// TestSweepRefusesUncommitted shows a sweep refuses to start, writing nothing, when the installed
// presets aren't the committed render; Restore alone installs it and verifies it.
func TestSweepRefusesUncommitted(t *testing.T) {
	h := newSweepHarness(t, gemmaE4B, targets(t))
	left := append(slices.Clone(h.committed), []byte("; a step left behind\n")...)
	if err := os.WriteFile(h.presets.path, left, 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := h.sweep.Run(t.Context())
	if rep != nil || err == nil || !strings.Contains(err.Error(), "evaluate sweep -restore") {
		t.Fatalf("Run = %v, %v; want refused, naming -restore", rep, err)
	}
	if b, _ := os.ReadFile(h.presets.path); !bytes.Equal(b, left) {
		t.Error("a refused sweep changed the presets file")
	}
	res, err := h.sweep.Restore(t.Context())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	h.checkRestored(t, &gate.SweepReport{Restore: res})
}

// TestLoadTargets shows the committed targets load, each role's target its prompt and answer, and
// a file missing a role's target or holding a key the schema doesn't define fails.
func TestLoadTargets(t *testing.T) {
	tg := targets(t)
	for role, want := range map[string]int{gate.RoleAgent: 8704, gate.RoleVision: 5172, gate.RoleAudio: 1956, gate.RoleEmbedding: 8192} {
		if got := tg.Of(role).Tokens(); got != want {
			t.Errorf("%s target %d tokens; want %d", role, got, want)
		}
	}
	bad := filepath.Join(t.TempDir(), "targets.toml")
	if err := os.WriteFile(bad, []byte("[agent]\nin = 8192\nout = 512\nsource = \"x\"\nextra = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := gate.LoadTargets(bad)
	if err == nil || !strings.Contains(err.Error(), "agent.extra") || !strings.Contains(err.Error(), "vision.in") {
		t.Errorf("LoadTargets(bad) = %v; want the undefined key and the missing roles named", err)
	}
}
