package passk

import (
	"cmp"
	"fmt"
	"io"
	"strings"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"
)

// WriteTable writes fits as a Markdown table, a row per harness and a column per capability,
// each cell the trials that passed out of those the harness ran, an excluded capability marked
// n/a, then the trials it ran of k, its invalid requests, and whether it fits.
func WriteTable(w io.Writer, fits []Fit) error {
	var b strings.Builder
	b.WriteString("| Harness | Version |")
	for _, c := range conform.Capabilities {
		b.WriteString(" " + c.Name + " |")
	}
	b.WriteString(" trials | invalid | fit |\n|---|---|")
	b.WriteString(strings.Repeat("---|", len(conform.Capabilities)+3) + "\n")
	for _, f := range fits {
		r := f.Row
		fmt.Fprintf(&b, "| %s | %s |", r.Consumer, cmp.Or(r.Version, "-"))
		for _, c := range r.Cells {
			fmt.Fprintf(&b, " %s |", cellText(c, r.K))
		}
		fit := "no"
		if f.Fits {
			fit = "yes"
		}
		fmt.Fprintf(&b, " %d/%d | %d | %s |\n", r.K, f.K, r.Invalid, fit)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteFits writes, a line each, every harness's verdict against the fit bar (Fit.Verdict).
func WriteFits(w io.Writer, fits []Fit) error {
	var b strings.Builder
	for _, f := range fits {
		fmt.Fprintf(&b, "%s: %s\n", f.Row.Consumer, f.Verdict())
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteReasons writes, a line each, the reason for every trial of rows that didn't pass and
// every invalid request in trials.
func WriteReasons(w io.Writer, rows []Row, trials map[string][]Trial) error {
	var b strings.Builder
	for _, r := range rows {
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "\n%s skipped a trial: %s", r.Consumer, oneLine(s))
		}
		for _, c := range r.Cells {
			for _, reason := range c.Reasons {
				fmt.Fprintf(&b, "\n%s %s: %s", r.Consumer, c.Capability, oneLine(reason))
			}
		}
		for k, t := range trials[r.Consumer] {
			for _, x := range t.Invalid {
				fmt.Fprintf(&b, "\n%s trial %d invalid: %s %s -> %d %s", r.Consumer, k+1, x.Method, x.Path, x.Status, truncate(oneLine(x.Response), 200))
			}
		}
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// cellText is a capability's trials that passed out of the k run, with n/a or skip noted when
// every trial that didn't pass was one.
func cellText(c Cell, k int) string {
	s := fmt.Sprintf("%d/%d", c.Pass, k)
	switch {
	case c.NA > 0 && c.NA == k-c.Pass:
		s += " n/a"
	case c.Skip > 0 && c.Skip == k-c.Pass:
		s += " skip"
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
