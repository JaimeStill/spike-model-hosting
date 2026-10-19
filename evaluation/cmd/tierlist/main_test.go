package main_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// bin is the tierlist tool, built once for the tests, which drive it as a user would.
var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tierlist-tool")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "tierlist")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir) // a temporary directory left behind fails no test
	os.Exit(code)
}

// run runs the tool with args and stdin, and returns its standard output, standard error and
// exit status.
func run(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
	}
	return out.String(), errOut.String(), code
}

// TestDraw draws a list from IDs on stdin: the dataset line becomes its header, and the IDs are
// tier's Generate's draw, whatever order they come in.
func TestDraw(t *testing.T) {
	in := "# dataset repo@abc set/test-*.parquet\nc\na\n\nd\nb\ne\n"
	out, errOut, code := run(t, in, "-tier", "asr-x", "-n", "3", "-seed", "7")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got, err := tier.ReadList(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	want := tier.Generate("asr-x", "repo@abc set/test-*.parquet", []string{"e", "d", "c", "b", "a"}, 3, 7)
	if got.Tier != want.Tier || got.Dataset != want.Dataset || got.Seed != 7 || strings.Join(got.IDs, ",") != strings.Join(want.IDs, ",") {
		t.Errorf("list %+v; want %+v", got, want)
	}
}

// TestDrawRefuses exits 2 on a usage error, with the usage, and 1 on IDs without a dataset line
// or fewer than the draw.
func TestDrawRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		in   string
		code int
	}{
		"no tier":    {[]string{"-n", "1"}, "# dataset d\na\n", 2},
		"no count":   {[]string{"-tier", "t"}, "# dataset d\na\n", 2},
		"no dataset": {[]string{"-tier", "t", "-n", "1"}, "a\n", 1},
		"too few":    {[]string{"-tier", "t", "-n", "3"}, "# dataset d\na\nb\na\n", 1},
		"stray line": {[]string{"-tier", "t", "-n", "1"}, "# dataset d\n# seed 3\na\n", 1},
	} {
		out, errOut, code := run(t, tc.in, tc.args...)
		if code != tc.code || out != "" || !strings.HasPrefix(errOut, "tierlist: ") || strings.Contains(errOut, "usage:") != (tc.code == 2) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want %d, nothing written, the error named", name, code, out, errOut, tc.code)
		}
	}
}
