package gate_test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// fakeUV is a uv executable that records each call and answers it from recorded results. An
// MTEB task is answered with testdata/mteb/<task>: the <task>.json and encoder.json of a live
// run of EmbeddingGemma 2 on the Framework, under MTEB 2.26.3. An ASR set is answered with
// testdata/asr/<set>: Inspect's JSON log of a live run of gemma-4-E4B on the Framework, trimmed
// to the utterances of testdata/tiers/asr-<set>.txt, and, given a bar, testdata/asr/<set>-stopped
// where there is one, a run early stopping ended. A vision tier is answered likewise with
// testdata/vision/<tier>: the Inspect JSON logs of a live run of Gemma 4 26B-A4B on the
// Framework, trimmed to testdata/tiers/vision-<tier>.txt, or testdata/vision/<tier>-stopped
// given a bar; a BFCL tier with testdata/agent/<tier>, a live run of gpt-oss-120b trimmed to
// testdata/tiers/agent-<tier>.txt, or testdata/agent/<tier>-stopped given a bar; a τ2 domain with
// testdata/tau2/<domain>, the summary (tau2.json) of a live run of gpt-oss-120b on the Framework,
// its user simulated by Sonnet through Claude Code 2.1.301, trimmed to
// testdata/tiers/agent-tau2-<domain>.txt's tasks, two trials each. Each result is stamped with the suites project's pin of MTEB, Inspect AI or
// inspect_evals, since the check refuses another's and a pin moves on while the recording stays
// the same.
type fakeUV struct {
	dir string
	// fail names a task, set, vision tier, BFCL tier or τ2 domain the fake exits 1 on, writing nothing; mteb, inspect
	// and evals, when set, are the versions stamped in place of the pins.
	fail, mteb, inspect, evals string
	// testdata, when set, is the directory the results are answered from in place of testdata.
	testdata string
}

func newFakeUV(t *testing.T) *fakeUV {
	return &fakeUV{dir: t.TempDir()}
}

// runner writes the fake's script as it is configured, and returns the UV that runs it.
func (f *fakeUV) runner() gate.UV {
	testdata, err := filepath.Abs(cmp.Or(f.testdata, "testdata"))
	if err != nil {
		panic(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s|%%s|%%s|%%s|%%s\n' "$*" "$HF_HOME" "$MTEB_CACHE" "$ASR_CACHE" "$INSPECT_EVALS_CACHE_DIR" >> %[1]q
kind= task= set= tier= out= bar= domain= peak=
while [ $# -gt 0 ]; do
	case $1 in
	peak) peak=1 ;;
	*/mteb_quality.py) kind=mteb ;;
	*/tau2_quality.py) kind=tau2 ;;
	--domain) domain=$2; shift ;;
	*/asr_quality.py) kind=asr ;;
	*/vision_quality.py) kind=vision ;;
	*/agent_quality.py) kind=agent ;;
	--task) task=$2; shift ;;
	--tier) tier=$2; shift ;;
	--set) set=$2; shift ;;
	--output) out=$2; shift ;;
	--bar) bar=$2; shift ;;
	esac
	shift
done
if [ -n "$peak" ]; then
	# The warm-up's peak media: a fixed image or clip, described as the scripts describe theirs.
	[ "$kind" = %[2]q ] && exit 1
	mkdir -p "$out" || exit 1
	case $kind in
	vision) printf '\211PNG\r\n\032\npeak image' > "$out/peak.png"
		printf '{"tier": "vision-vstar", "id": "83", "file": "peak.png", "width": 3300, "height": 2400}\n' > "$out/peak.json" ;;
	asr) printf 'RIFF peak clip' > "$out/peak.wav"
		printf '{"tier": "asr-earnings22", "id": "4483338/86.wav", "file": "peak.wav", "audio_s": 29.5}\n' > "$out/peak.json" ;;
	*) exit 2 ;;
	esac
	exit 0
fi
case $kind in
mteb)
	[ "$task" = %[2]q ] && exit 1
	mkdir -p "$out" && cp %[3]q/mteb/"$task"/* "$out"/ || exit 1
	sed -i 's/"mteb_version": "[^"]*"/"mteb_version": "%[4]s"/' "$out/$task.json" ;;
asr)
	[ "$set" = %[2]q ] && exit 1
	src=%[3]q/asr/"$set"
	[ -n "$bar" ] && [ -d "$src-stopped" ] && src=$src-stopped
	mkdir -p "$out" && cp "$src"/*.json "$out"/ || exit 1
	sed -i 's/"inspect_ai": "[^"]*"/"inspect_ai": "%[5]s"/' "$out"/*.json ;;
vision|agent)
	[ "$tier" = %[2]q ] && exit 1
	src=%[3]q/$kind/"$tier"
	[ -n "$bar" ] && [ -d "$src-stopped" ] && src=$src-stopped
	mkdir -p "$out" && cp "$src"/*.json "$out"/ || exit 1
	sed -i -e 's/"inspect_ai": "[^"]*"/"inspect_ai": "%[5]s"/' -e 's/"inspect_evals": "[^"]*"/"inspect_evals": "%[6]s"/' "$out"/*.json ;;
tau2)
	[ "$domain" = %[2]q ] && exit 1
	mkdir -p "$out" && cp %[3]q/tau2/"$domain"/* "$out"/ || exit 1
	sed -i -e 's/"inspect_ai": "[^"]*"/"inspect_ai": "%[5]s"/' -e 's/"inspect_evals": "[^"]*"/"inspect_evals": "%[6]s"/' "$out"/*.json ;;
*) exit 2 ;;
esac
`, filepath.Join(f.dir, "calls"), f.fail, testdata, cmp.Or(f.mteb, gate.MTEBVersion), cmp.Or(f.inspect, gate.InspectVersion),
		cmp.Or(f.evals, gate.InspectEvalsVersion))
	exe := filepath.Join(f.dir, "uv")
	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		panic(err)
	}
	// τ2's simulated user needs a Claude Code login in its configuration directory.
	login := filepath.Join(f.dir, "claude")
	if err := os.MkdirAll(login, 0o700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(login, ".credentials.json"), []byte("{}"), 0o600); err != nil {
		panic(err)
	}
	return gate.UV{Exe: exe, Python: "/pinned/python", Project: "/module/evaluation/suites", Cache: "/state/cache",
		Claude: "/pinned/claude", ClaudeConfig: login}
}

// calls returns each call of the quality stage's, its arguments, HF_HOME, MTEB_CACHE, ASR_CACHE
// and INSPECT_EVALS_CACHE_DIR, '|'-separated, in order; setCalls returns the set stage's, those
// whose output is under a run directory's set/, deepCalls the deep stage's, under its deep/, and
// peakCalls the warm-up's, under its warm/.
func (f *fakeUV) calls(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range f.allCalls(t) {
		if !setCall.MatchString(c) && !deepCall.MatchString(c) && !peakCall.MatchString(c) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeUV) deepCalls(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range f.allCalls(t) {
		if deepCall.MatchString(c) {
			out = append(out, c)
		}
	}
	return out
}

// deepCall matches a deep stage's call, by its output under the run directory's deep/.
var deepCall = regexp.MustCompile(`--output \S*/deep/`)

func (f *fakeUV) peakCalls(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range f.allCalls(t) {
		if peakCall.MatchString(c) {
			out = append(out, c)
		}
	}
	return out
}

// peakCall matches the warm-up's call, by its output under the run directory's warm/.
var peakCall = regexp.MustCompile(`--output \S*/warm/`)

func (f *fakeUV) setCalls(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range f.allCalls(t) {
		if setCall.MatchString(c) {
			out = append(out, c)
		}
	}
	return out
}

// setCall matches a set stage's call, by its output under the run directory's set/.
var setCall = regexp.MustCompile(`--output \S*/set/(alone|concurrent)/`)

func (f *fakeUV) allCalls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// The recorded main scores, from testdata/mteb. Banking77Classification's result carries
// Python's bare NaN, for its undefined ap, which ReadMTEB reads as null.
var recorded = map[string]struct {
	score  float64
	metric string
}{
	"STSBenchmark":            {0.874775, "cosine_spearman"},
	"Banking77Classification": {0.922695, "accuracy"},
	"SciFact":                 {0.85153, "ndcg_at_10"},
	"NFCorpus":                {0.36582, "ndcg_at_10"},
}

// TestQualityEmbedding runs the embedding model's quality stage with no bars: each MTEB task
// runs in full through uv, its main score is read from MTEB's result and recorded with its
// version (the pinned one, which the fake stamps), the prompts the encoder applied and its wall
// time, and, with no bar, it passes.
func TestQualityEmbedding(t *testing.T) {
	h := newHarness(t, embedding)
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q\n%+v", got, want, rep.Stages)
	}
	q := rep.Stage(gate.StageQuality)
	if len(q.Checks) != len(gate.EmbeddingTasks) {
		t.Fatalf("%d quality checks; want one a task, %d", len(q.Checks), len(gate.EmbeddingTasks))
	}
	for i, task := range gate.EmbeddingTasks {
		c := q.Checks[i]
		r := recorded[task]
		if c.Name != task || c.Role != gate.RoleEmbedding || c.Measured == nil || *c.Measured != r.score || c.Unit != r.metric {
			t.Errorf("check %d %+v; want %s's %s %v", i, c, task, r.metric, r.score)
		}
		if c.Floor != nil || c.Op != "" || !c.Passed {
			t.Errorf("%s: floor %v op %q passed %v; want no bar, recorded, passed", task, c.Floor, c.Op, c.Passed)
		}
		for _, want := range []string{"mteb " + gate.MTEBVersion, "early stop n/a", "no bar: recorded, never fails"} {
			if !strings.Contains(c.Detail, want) {
				t.Errorf("%s: detail %q lacks %q", task, c.Detail, want)
			}
		}
		if _, ok := c.Values["wall_s"]; !ok {
			t.Errorf("%s: no wall_s in %v", task, c.Values)
		}
	}
	// The prompts recorded are the encoder's, by prompt type.
	if d := q.Checks[0].Detail; !strings.Contains(d, `prompts all="task: sentence similarity | query: "`) {
		t.Errorf("STSBenchmark detail %q; want its symmetric prompt", d)
	}
	if d := q.Checks[2].Detail; !strings.Contains(d, `document="title: {title or none} | text: ", query="task: search result | query: "`) {
		t.Errorf("SciFact detail %q; want its query and document prompts", d)
	}

	// Each task ran once, in order, through the suites project at the pinned Python, given the
	// model's prompts from the profile in its output's prompts.json, its output in the run
	// directory's mteb/<task>, the caches under the state directory's.
	calls := h.uv.calls(t)
	if len(calls) != len(gate.EmbeddingTasks) {
		t.Fatalf("uv calls %q; want one a task", calls)
	}
	wantPrompts := h.runner.Profile.Model(embedding).Prompts.Embedding
	if wantPrompts["STS"] != "task: sentence similarity | query: " {
		t.Fatalf("the test profile's embedding prompts %v lack STS's", wantPrompts)
	}
	for i, task := range gate.EmbeddingTasks {
		out := filepath.Join(h.runner.Dir, "mteb", task)
		var prompts map[string]string
		if b, err := os.ReadFile(filepath.Join(out, "prompts.json")); err != nil {
			t.Error(err)
		} else if err := json.Unmarshal(b, &prompts); err != nil || !maps.Equal(prompts, wantPrompts) {
			t.Errorf("%s: prompts.json %s (%v); want the profile's %v", task, b, err, wantPrompts)
		}
		want := fmt.Sprintf("run --project /module/evaluation/suites --locked --no-python-downloads --python /pinned/python python "+
			"/module/evaluation/suites/mteb_quality.py --router %s --model %s --task %s --prompts %s --output %s|/state/cache/huggingface|/state/cache/mteb|/state/cache/asr|/state/cache/inspect_evals",
			h.router.srv.URL, embedding, task, filepath.Join(out, "prompts.json"), out)
		if calls[i] != want {
			t.Errorf("call %d:\n got %s\nwant %s", i, calls[i], want)
		}
	}

	var sum bytes.Buffer
	if err := gate.WriteSummary(&sum, rep); err != nil {
		t.Fatal(err)
	}
	if want := "ok   embedding SciFact: 0.852 ndcg_at_10, no floor"; !strings.Contains(sum.String(), want) {
		t.Errorf("summary lacks %q:\n%s", want, sum.String())
	}
}

// TestQualityBars judges each task's main score against its bar in the bars, by task name: one
// under its bar fails the stage, and the run stops there.
func TestQualityBars(t *testing.T) {
	h := newHarness(t, embedding)
	h.runner.Bars = tier.Bars{"SciFact": {Value: 0.9}, "STSBenchmark": {Value: 0.8}}
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality fail", "set not run", "deep not run"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q", got, want)
	}
	if c := check(t, rep, gate.StageQuality, gate.RoleEmbedding, "SciFact"); c.Passed || *c.Floor != 0.9 || c.Op != gate.AtLeast {
		t.Errorf("SciFact %+v; want a failure against >= 0.9", c)
	}
	sts := check(t, rep, gate.StageQuality, gate.RoleEmbedding, "STSBenchmark")
	if !sts.Passed || *sts.Floor != 0.8 || strings.Contains(sts.Detail, "no bar") {
		t.Errorf("STSBenchmark %+v; want a pass against >= 0.8", sts)
	}
	if c := check(t, rep, gate.StageQuality, gate.RoleEmbedding, "NFCorpus"); !c.Passed || c.Floor != nil {
		t.Errorf("NFCorpus %+v; want recorded with no bar", c)
	}
}

// TestQualityBroken shows a task uv fails on, whose result another MTEB wrote, or whose bar is a
// ceiling, fails its check, naming why, and the other tasks still run.
func TestQualityBroken(t *testing.T) {
	cases := map[string]struct {
		fail, version, want string
		bars                tier.Bars
	}{
		"uv fails":      {fail: "SciFact", want: "uv run mteb_quality.py --task SciFact"},
		"other version": {version: "2.25.0", want: "MTEB 2.25.0's, not the pinned " + gate.MTEBVersion + "'s"},
		"a ceiling":     {bars: tier.Bars{"SciFact": {Value: 0.9, AtMost: true}}, want: "its bar belongs under [bars]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, embedding)
			h.uv.fail, h.uv.mteb = tc.fail, tc.version
			h.runner.MTEB, h.runner.Bars = h.uv.runner(), tc.bars
			rep := h.run(t, "", nil)
			if s := rep.Stage(gate.StageQuality); s.Status != gate.StatusFail || len(s.Checks) != len(gate.EmbeddingTasks) {
				t.Fatalf("quality %+v; want failed, every task checked", s)
			}
			c := check(t, rep, gate.StageQuality, gate.RoleEmbedding, "SciFact")
			if c.Passed || !strings.Contains(c.Detail, tc.want) {
				t.Errorf("SciFact %+v; want a failure naming %q", c, tc.want)
			}
		})
	}
}

// TestReadMTEB reads a result's main score as MTEB's get_score takes it, the mean over every
// split and subset, and refuses a result that isn't the task's or holds no score.
func TestReadMTEB(t *testing.T) {
	write := func(t *testing.T, task, result string) string {
		t.Helper()
		dir := t.TempDir()
		enc := `{"main_score": "accuracy", "prompts": {"all": "task: classification | query: "}}`
		if err := os.WriteFile(filepath.Join(dir, "encoder.json"), []byte(enc), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, task+".json"), []byte(result), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dir := write(t, "T", `{"task_name": "T", "mteb_version": "2.26.4", "scores": {
		"test": [{"main_score": 0.5, "hf_subset": "a"}, {"main_score": 0.7, "hf_subset": "b"}],
		"dev": [{"main_score": 0.9, "hf_subset": "a"}]}}`)
	res, err := gate.ReadMTEB(dir, "T")
	if err != nil || res.Entries != 3 || res.MainScore < 0.6999 || res.MainScore > 0.7001 || res.Metric != "accuracy" || res.Version != "2.26.4" {
		t.Errorf("ReadMTEB = %+v, %v; want the mean 0.7 of 3 entries", res, err)
	}
	for name, result := range map[string]string{
		"another task": `{"task_name": "U", "scores": {"test": [{"main_score": 0.5}]}}`,
		"no scores":    `{"task_name": "T", "scores": {}}`,
		"no main":      `{"task_name": "T", "scores": {"test": [{"accuracy": 0.5}]}}`,
	} {
		if _, err := gate.ReadMTEB(write(t, "T", result), "T"); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
}

// recordedASR are each ASR fixture's scored utterances, word errors and reference words, as
// testdata/asr/<set>'s Inspect log holds them; its utterances are testdata/tiers/asr-<set>.txt's.
// Each log is a live run's with the events, messages and attachments dropped from its samples
// and every sample but the tier's taken out: the first three of the committed tier list, from the
// gated run's, and AMI's ten from a run of their own, beside the same ten with a bar of 0.001,
// testdata/asr/ami-stopped, which early stopping ended after four. The logs' results keep the
// whole run's metrics, which ReadASR doesn't read.
var recordedASR = map[string]struct{ utterances, errors, words int }{
	"librispeech-clean": {3, 2, 44},
	"librispeech-other": {3, 3, 101},
	"voxpopuli":         {3, 8, 77},
	"ami":               {10, 11, 74},
	"earnings22":        {3, 9, 53},
	"fleurs-en":         {3, 2, 59},
}

// The fixtures' datasets and normalizer, as asr_quality.py records them.
const (
	esbDataset = "dataset hf-audio/esb-datasets-test-only-sorted@de79e57adf9983993bcc72586d7313460825f5eb "
	normalizer = "normalizer huggingface/open_asr_leaderboard@1b85e2790d532b074888caa4add3ee5bef2f60cd"
)

// TestQualityAudio runs the audio model's quality stage with no bars: each ASR set's tier runs
// whole through uv, its corpus WER is read from Inspect's log, every word error over every
// reference word, and recorded with its RTFx, counts, dataset, normalizer and Inspect release,
// and, with no bar, it passes. FLEURS English records the card's FLEURS figure beside it.
func TestQualityAudio(t *testing.T) {
	h := newHarness(t, gemmaE4B)
	// The ASR task's prompt is the profile's: a changed one is the one passed.
	const prompt = "Transcribe this clip, as the profile says."
	h.runner.Profile.Model(gemmaE4B).Prompts.ASR = prompt
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q\n%+v", got, want, rep.Stages)
	}
	q := rep.Stage(gate.StageQuality)
	if len(q.Checks) != len(gate.AudioSets) {
		t.Fatalf("%d quality checks; want one a set, %d", len(q.Checks), len(gate.AudioSets))
	}
	for i, set := range gate.AudioSets {
		c, r := q.Checks[i], recordedASR[set]
		wer := float64(r.errors) / float64(r.words)
		if c.Name != gate.ASRTier(set) || c.Role != gate.RoleAudio || c.Measured == nil || *c.Measured != wer || c.Unit != "WER" {
			t.Errorf("check %d %+v; want %s's WER %v", i, c, gate.ASRTier(set), wer)
		}
		if c.Floor != nil || c.Op != "" || !c.Passed {
			t.Errorf("%s: floor %v op %q passed %v; want no bar, recorded, passed", set, c.Floor, c.Op, c.Passed)
		}
		if c.Values["utterances"] != float64(r.utterances) || c.Values["errors"] != float64(r.errors) || c.Values["words"] != float64(r.words) {
			t.Errorf("%s: values %v; want %d utterances, %d errors over %d words", set, c.Values, r.utterances, r.errors, r.words)
		}
		if rtfx := c.Values["rtfx"]; rtfx <= 0 || rtfx != c.Values["audio_s"]/c.Values["request_s"] {
			t.Errorf("%s: rtfx %v; want the audio's seconds over the requests' in %v", set, rtfx, c.Values)
		}
		for _, want := range []string{"no bar: recorded, never fails", "inspect " + gate.InspectVersion, normalizer, "dataset "} {
			if !strings.Contains(c.Detail, want) {
				t.Errorf("%s: detail %q lacks %q", set, c.Detail, want)
			}
		}
		_, card := c.Values["card_wer"]
		if card != (set == "fleurs-en") {
			t.Errorf("%s: card_wer recorded %v; want it on fleurs-en alone", set, card)
		}
	}
	if d := q.Checks[0].Detail; !strings.Contains(d, esbDataset+"librispeech/test.clean-*.parquet") {
		t.Errorf("librispeech-clean detail %q; want its dataset and revision", d)
	}
	fleurs := check(t, rep, gate.StageQuality, gate.RoleAudio, "asr-fleurs-en")
	if fleurs.Values["card_wer"] != 0.08 || !strings.Contains(fleurs.Detail, `"FLEURS (lower is better)" 0.08`) ||
		!strings.Contains(fleurs.Detail, "google/fleurs@70bb2e84b976b7e960aa89f1c648e09c59f894dd") {
		t.Errorf("fleurs-en %+v; want the card's 0.08 beside ours, and FLEURS's revision", fleurs)
	}

	// Each set ran once, in order, through the suites project at the pinned Python, with its
	// tier list, the profile's prompt (the one fitness and capacity send), no bar, its
	// log in the run directory's asr/<set>, the caches under the state directory's.
	calls := h.uv.calls(t)
	if len(calls) != len(gate.AudioSets) {
		t.Fatalf("uv calls %q; want one a set", calls)
	}
	for i, set := range gate.AudioSets {
		want := fmt.Sprintf("run --project /module/evaluation/suites --locked --no-python-downloads --python /pinned/python python "+
			"/module/evaluation/suites/asr_quality.py eval --router %s --model %s --set %s --tier %s --output %s --prompt %s|/state/cache/huggingface|/state/cache/mteb|/state/cache/asr|/state/cache/inspect_evals",
			h.router.srv.URL, gemmaE4B, set, filepath.Join(h.runner.Tiers, gate.ASRTier(set)+".txt"), filepath.Join(h.runner.Dir, "asr", set), prompt)
		if calls[i] != want {
			t.Errorf("call %d:\n got %s\nwant %s", i, calls[i], want)
		}
	}
}

// TestQualityAudioBars judges each set's WER against its ceiling: a set with a bar passes it to
// asr_quality.py, whose early stop ends the set once the WER's lower bound is above it, failing
// the check and the stage; a set under its bar runs in full and passes.
func TestQualityAudioBars(t *testing.T) {
	h := newHarness(t, gemmaE4B)
	h.runner.Bars = tier.Bars{"asr-ami": {Value: 0.001, AtMost: true}, "asr-librispeech-clean": {Value: 0.5, AtMost: true}}
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality fail", "set not run", "deep not run"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q", got, want)
	}
	ami := check(t, rep, gate.StageQuality, gate.RoleAudio, "asr-ami")
	if ami.Passed || ami.Op != gate.AtMost || *ami.Floor != 0.001 || !strings.Contains(ami.Detail, "early stop after 4: the 95% lower bound") {
		t.Errorf("asr-ami %+v; want stopped early, failed against <= 0.001", ami)
	}
	if _, ok := ami.Values["wer_lower_bound"]; !ok || ami.Values["utterances"] != 4 {
		t.Errorf("asr-ami values %v; want the bound and the 4 utterances run", ami.Values)
	}
	clean := check(t, rep, gate.StageQuality, gate.RoleAudio, "asr-librispeech-clean")
	if !clean.Passed || clean.Op != gate.AtMost || *clean.Floor != 0.5 || !strings.Contains(clean.Detail, "early stop: ran in full") {
		t.Errorf("asr-librispeech-clean %+v; want a pass against <= 0.5, run in full", clean)
	}
	if c := check(t, rep, gate.StageQuality, gate.RoleAudio, "asr-voxpopuli"); !c.Passed || c.Floor != nil {
		t.Errorf("asr-voxpopuli %+v; want recorded with no bar", c)
	}
	var barred []string
	for _, c := range h.uv.calls(t) {
		if strings.Contains(c, "--bar") {
			barred = append(barred, c)
		}
	}
	if len(barred) != 2 || !strings.Contains(barred[0], "--set librispeech-clean ") || !strings.HasSuffix(strings.Split(barred[0], "|")[0], "--bar 0.5") ||
		!strings.Contains(barred[1], "--set ami ") || !strings.HasSuffix(strings.Split(barred[1], "|")[0], "--bar 0.001") {
		t.Errorf("calls with a bar %q; want librispeech-clean's 0.5 and ami's 0.001", barred)
	}
}

// TestQualityAudioBroken shows a set uv fails on, whose log another Inspect wrote or doesn't
// match its tier list, or whose bar is a floor, fails its check, naming why, and the other sets
// still run.
func TestQualityAudioBroken(t *testing.T) {
	cases := map[string]struct {
		fail, inspect, want string
		bars                tier.Bars
		extraID             bool
	}{
		"uv fails":      {fail: "ami", want: "uv run asr_quality.py eval --set ami"},
		"other inspect": {inspect: "0.3.200", want: "Inspect 0.3.200's, not the pinned " + gate.InspectVersion + "'s"},
		"a floor":       {bars: tier.Bars{"asr-ami": {Value: 0.2}}, want: "its bar belongs under [bars_at_most]"},
		"list differs":  {extraID: true, want: "aren't the tier list's 11"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, gemmaE4B)
			h.uv.fail, h.uv.inspect = tc.fail, tc.inspect
			h.runner.ASR, h.runner.Bars = h.uv.runner(), tc.bars
			if tc.extraID {
				h.runner.Tiers = withExtraID(t, h.runner.Tiers, "asr-ami")
			}
			rep := h.run(t, "", nil)
			if s := rep.Stage(gate.StageQuality); s.Status != gate.StatusFail || len(s.Checks) != len(gate.AudioSets) {
				t.Fatalf("quality %+v; want failed, every set checked", s)
			}
			c := check(t, rep, gate.StageQuality, gate.RoleAudio, "asr-ami")
			if c.Passed || !strings.Contains(c.Detail, tc.want) {
				t.Errorf("asr-ami %+v; want a failure naming %q", c, tc.want)
			}
			if tc.bars != nil {
				for _, call := range h.uv.calls(t) {
					if strings.Contains(call, "--set ami ") {
						t.Errorf("ami ran with a floor for a bar: %s", call)
					}
				}
			}
		})
	}
}

// withExtraID copies the tier lists in dir to a new directory, adding an ID to the named list.
func withExtraID(t *testing.T, dir, name string) string {
	t.Helper()
	out := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == name+".txt" {
			b = append(b, "AMI_extra_utterance\n"...)
		}
		if err := os.WriteFile(filepath.Join(out, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// TestReadASR sums a log's utterances, and refuses a log of a run that didn't succeed, with an
// utterance that errored or went unscored, or beside another log.
func TestReadASR(t *testing.T) {
	src := filepath.Join("testdata", "asr", "ami")
	entries, err := os.ReadDir(src)
	if err != nil || len(entries) != 1 {
		t.Fatalf("testdata/asr/ami: %v, %d entries; want one log", err, len(entries))
	}
	logName := entries[0].Name()
	b, err := os.ReadFile(filepath.Join(src, logName))
	if err != nil {
		t.Fatal(err)
	}
	// write writes the fixture's log, edited by edit, into a new directory.
	write := func(t *testing.T, edit func(log map[string]any)) string {
		t.Helper()
		var log map[string]any
		if err := json.Unmarshal(b, &log); err != nil {
			t.Fatal(err)
		}
		edit(log)
		out, err := json.Marshal(log)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, logName), out, 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	r := recordedASR["ami"]
	res, err := gate.ReadASR(write(t, func(map[string]any) {}))
	if err != nil || len(res.IDs) != r.utterances || res.Errors != r.errors || res.Words != r.words || res.Set != "ami" || res.Stopped != nil {
		t.Errorf("ReadASR = %+v, %v; want ami's %d utterances, %d errors over %d words", res, err, r.utterances, r.errors, r.words)
	}
	sample := func(log map[string]any) map[string]any { return log["samples"].([]any)[0].(map[string]any) }
	for name, edit := range map[string]func(map[string]any){
		"run errored":     func(log map[string]any) { log["status"] = "error" },
		"utterance error": func(log map[string]any) { sample(log)["error"] = map[string]any{"message": "boom"} },
		"unscored":        func(log map[string]any) { delete(sample(log), "scores") },
		"no time":         func(log map[string]any) { delete(sample(log)["output"].(map[string]any), "time") },
	} {
		if _, err := gate.ReadASR(write(t, edit)); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
	two := write(t, func(map[string]any) {})
	if err := os.WriteFile(filepath.Join(two, "2026-01-01T00-00-00-00-00_asr_x.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.ReadASR(two); err == nil {
		t.Error("two logs: read without error")
	}
}
