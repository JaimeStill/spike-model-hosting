// Command tierlist draws a quality tier's seeded sample list from a dataset's IDs and writes it
// in package tier's list format, so a committed list can be drawn again from its seed.
//
// Usage:
//
//	tierlist -tier name -n count -seed seed < ids
//
// It reads the dataset's IDs from stdin, one a line, after the line "# dataset <name>" that
// names the dataset; blank lines are skipped. It draws -n of them with -seed through tier's
// Generate, which sorts and deduplicates them first, so the list depends only on the IDs and
// the seed, and writes the list to stdout. Every committed list in evaluation/tiers is drawn
// from the `ids` command of its suite in the suites project, which writes the dataset line and
// IDs in this form: asr_quality.py's for the audio role's lists, vision_quality.py's for the
// vision role's, and agent_quality.py's and tau2_quality.py's for the agent role's BFCL and τ2
// lists (mise run //evaluation:asr-tiers, vision-tiers, agent-tiers and tau2-tiers, in the
// consumers environment).
//
// It exits 0 on a list written, 1 when the IDs can't be read or are fewer than -n, and 2 on a
// usage error.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// errUsage marks an error in how the tool was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

const usage = "usage:\n  tierlist -tier name -n count -seed seed < ids\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run draws the list args name from the IDs on stdin, and returns the exit status.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	err := draw(args, stdin, stdout)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "tierlist: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "tierlist: %v\n", err)
		return 1
	}
}

func draw(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("tierlist", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("tier", "", "the tier's name")
	n := fs.Int("n", 0, "the samples to draw")
	seed := fs.Uint64("seed", 0, "the seed to draw them with")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	switch {
	case fs.NArg() != 0:
		return fmt.Errorf("%w: unexpected arguments %q", errUsage, fs.Args())
	case *name == "":
		return fmt.Errorf("%w: name the tier with -tier", errUsage)
	case *n <= 0:
		return fmt.Errorf("%w: -n must be a count above 0", errUsage)
	}

	dataset := ""
	var ids []string
	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "# dataset "):
			dataset = strings.TrimPrefix(line, "# dataset ")
		case strings.HasPrefix(line, "#"):
			return fmt.Errorf("the IDs: %q is no line they hold", line)
		default:
			ids = append(ids, line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	l := tier.Generate(*name, dataset, ids, *n, *seed)
	switch {
	case dataset == "":
		return errors.New(`the IDs carry no "# dataset <name>" line`)
	case len(l.IDs) < *n:
		return fmt.Errorf("the dataset holds %d distinct IDs, fewer than the %d to draw", len(l.IDs), *n)
	}
	return l.Write(stdout)
}
