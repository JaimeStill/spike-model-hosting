package gate_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// fakeUV is a uv executable that records each call and answers an MTEB task with its recorded
// result, testdata/mteb/<task>: MTEB 2.26.3's <task>.json and the encoder's encoder.json from a
// live run of EmbeddingGemma 2 on the Framework.
type fakeUV struct {
	dir string
	// fail names a task the fake exits 1 on, writing nothing; version, when set, replaces the
	// result's mteb_version.
	fail, version string
}

func newFakeUV(t *testing.T) *fakeUV {
	return &fakeUV{dir: t.TempDir()}
}

// runner writes the fake's script as it is configured, and returns the UV that runs it.
func (f *fakeUV) runner() gate.UV {
	fixtures, err := filepath.Abs("testdata/mteb")
	if err != nil {
		panic(err)
	}
	version := ""
	if f.version != "" {
		version = fmt.Sprintf(`sed -i 's/"mteb_version": "2.26.3"/"mteb_version": "%s"/' "$out/$task.json"`, f.version)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s|%%s|%%s\n' "$*" "$HF_HOME" "$MTEB_CACHE" >> %[1]q
task= out=
while [ $# -gt 0 ]; do
	case $1 in
	--task) task=$2; shift ;;
	--output) out=$2; shift ;;
	esac
	shift
done
[ "$task" = %[2]q ] && exit 1
mkdir -p "$out" && cp %[3]q/"$task"/* "$out"/ || exit 1
%[4]s
`, filepath.Join(f.dir, "calls"), f.fail, fixtures, version)
	exe := filepath.Join(f.dir, "uv")
	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		panic(err)
	}
	return gate.UV{Exe: exe, Python: "/pinned/python", Project: "/module/evaluation/suites", Cache: "/state/cache"}
}

// calls returns each call's arguments, HF_HOME and MTEB_CACHE, '|'-separated, in order.
func (f *fakeUV) calls(t *testing.T) []string {
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
// version, the prompts the encoder applied and its wall time, and, with no bar, it passes.
func TestQualityEmbedding(t *testing.T) {
	h := newHarness(t, embedding)
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set not built", "deep not built"}
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
		for _, want := range []string{"mteb 2.26.3", "early stop n/a", "no bar: recorded, never fails"} {
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

	// Each task ran once, in order, through the suites project at the pinned Python, its output
	// in the run directory's mteb/<task>, the caches under the state directory's.
	calls := h.uv.calls(t)
	if len(calls) != len(gate.EmbeddingTasks) {
		t.Fatalf("uv calls %q; want one a task", calls)
	}
	for i, task := range gate.EmbeddingTasks {
		want := fmt.Sprintf("run --project /module/evaluation/suites --locked --no-python-downloads --python /pinned/python python "+
			"/module/evaluation/suites/mteb_quality.py --router %s --model %s --task %s --output %s|/state/cache/huggingface|/state/cache/mteb",
			h.router.srv.URL, embedding, task, filepath.Join(h.runner.Dir, "mteb", task))
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
	h.runner.Bars = tier.Bars{"SciFact": 0.9, "STSBenchmark": 0.8}
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

// TestQualityBroken shows a task uv fails on, or whose result another MTEB wrote, fails its
// check, naming why, and the other tasks still run.
func TestQualityBroken(t *testing.T) {
	cases := map[string]struct {
		fail, version, want string
	}{
		"uv fails":      {fail: "SciFact", want: "uv run mteb_quality.py --task SciFact"},
		"other version": {version: "2.25.0", want: "MTEB 2.25.0's, not the pinned 2.26.3's"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, embedding)
			h.uv.fail, h.uv.version = tc.fail, tc.version
			h.runner.MTEB = h.uv.runner()
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
	dir := write(t, "T", `{"task_name": "T", "mteb_version": "2.26.3", "scores": {
		"test": [{"main_score": 0.5, "hf_subset": "a"}, {"main_score": 0.7, "hf_subset": "b"}],
		"dev": [{"main_score": 0.9, "hf_subset": "a"}]}}`)
	res, err := gate.ReadMTEB(dir, "T")
	if err != nil || res.Entries != 3 || res.MainScore < 0.6999 || res.MainScore > 0.7001 || res.Metric != "accuracy" || res.Version != "2.26.3" {
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
