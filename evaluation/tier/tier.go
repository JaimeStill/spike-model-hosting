package tier

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Z95 is the standard normal quantile of a one-sided 95% bound.
const Z95 = 1.6448536269514722

// UpperBound returns the one-sided 95% Wilson score upper bound on the pass rate of passes out
// of n samples, and 1 for no samples.
func UpperBound(passes, n int) float64 {
	if n <= 0 {
		return 1
	}
	p := float64(passes) / float64(n)
	nf, z2 := float64(n), Z95*Z95
	centre := p + z2/(2*nf)
	margin := Z95 * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))
	return min(1, (centre+margin)/(1+z2/nf))
}

// List is a tier's seeded sample list: the sample IDs, in the order they are sent.
type List struct {
	Tier, Dataset string
	Seed          uint64
	IDs           []string
}

// Generate draws n sample IDs from a dataset's ids with seed, in the order a tier sends them;
// n <= 0, or n past the dataset's size, draws them all. The ids are sorted and deduplicated
// first, so the same IDs and seed give the same list whatever order the dataset lists them in.
func Generate(tierName, dataset string, ids []string, n int, seed uint64) List {
	pool := slices.Compact(slices.Sorted(slices.Values(ids)))
	r := rand.New(rand.NewPCG(seed, 0))
	r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if n > 0 && n < len(pool) {
		pool = pool[:n]
	}
	return List{Tier: tierName, Dataset: dataset, Seed: seed, IDs: pool}
}

// Write writes l in the list format: its header, then one ID a line.
func (l List) Write(w io.Writer) error {
	bw := bufio.NewWriter(w)
	_, _ = fmt.Fprintf(bw, "# tier %s\n# dataset %s\n# seed %d\n", l.Tier, l.Dataset, l.Seed)
	for _, id := range l.IDs {
		_, _ = fmt.Fprintln(bw, id)
	}
	return bw.Flush()
}

// ReadList reads a list in the format Write writes. Blank lines are skipped; the header's three
// lines are required, and an ID listed twice fails.
func ReadList(r io.Reader) (List, error) {
	var l List
	header := map[string]bool{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			key, value, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "#")), " ")
			switch key {
			case "tier":
				l.Tier = value
			case "dataset":
				l.Dataset = value
			case "seed":
				s, err := strconv.ParseUint(value, 10, 64)
				if err != nil {
					return l, fmt.Errorf("tier list line %d: seed %q: %w", n, value, err)
				}
				l.Seed = s
			default:
				return l, fmt.Errorf("tier list line %d: %q is not a header the format defines", n, line)
			}
			header[key] = true
		default:
			if seen[line] {
				return l, fmt.Errorf("tier list line %d: %q is listed twice", n, line)
			}
			seen[line] = true
			l.IDs = append(l.IDs, line)
		}
	}
	if err := sc.Err(); err != nil {
		return l, err
	}
	for _, key := range []string{"tier", "dataset", "seed"} {
		if !header[key] {
			return l, fmt.Errorf("tier list: no %q header", key)
		}
	}
	return l, nil
}

// Bar is what a tier or task is judged against: a floor that a pass rate or a higher-is-better
// score must reach, or, AtMost, a ceiling that a lower-is-better measure, such as a word error
// rate, must not exceed. Either is in [0, 1].
type Bar struct {
	Value  float64
	AtMost bool
}

// Bars are the bars by tier or task name.
type Bars map[string]Bar

// LoadBars reads the bars file at path: name = bar keys, floors under [bars] and ceilings under
// [bars_at_most]. A key outside the two tables, a name in both, or a bar outside [0, 1] fails.
func LoadBars(path string) (Bars, error) {
	var f struct {
		Floors   map[string]float64 `toml:"bars"`
		Ceilings map[string]float64 `toml:"bars_at_most"`
	}
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return nil, fmt.Errorf("bars: %w", err)
	}
	var errs []error
	for _, k := range md.Undecoded() {
		errs = append(errs, fmt.Errorf("bars %s: %s: not a key the file defines", path, k))
	}
	bars := Bars{}
	for table, values := range map[string]map[string]float64{"bars": f.Floors, "bars_at_most": f.Ceilings} {
		for name, v := range values {
			if v < 0 || v > 1 || math.IsNaN(v) {
				errs = append(errs, fmt.Errorf("bars %s: %s.%s: %v is outside [0, 1]", path, table, name, v))
			}
			if _, dup := bars[name]; dup {
				errs = append(errs, fmt.Errorf("bars %s: %s is under both [bars] and [bars_at_most]", path, name))
			}
			bars[name] = Bar{Value: v, AtMost: table == "bars_at_most"}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return bars, nil
}

// Bar returns the bar for name, a tier or task, and false when the bars hold none for it.
func (b Bars) Bar(name string) (Bar, bool) {
	v, ok := b[name]
	return v, ok
}

// Judge sends one sample and reports whether it passed. An error ends the run: it is a sample
// that couldn't be judged, not one that failed.
type Judge func(ctx context.Context, id string) (bool, error)

// Result is a tier's run.
type Result struct {
	Tier string `json:"tier"`
	// Size is the list's length; Screening, its screening fifth; Run, the samples sent.
	Size      int `json:"size"`
	Screening int `json:"screening"`
	Run       int `json:"run"`
	Passes    int `json:"passes"`
	// Rate is Passes over Run; Upper, the one-sided 95% Wilson bound on it when the run ended.
	Rate  float64 `json:"rate"`
	Upper float64 `json:"upper_bound"`
	// Bar is the tier's bar, nil when it has none.
	Bar *float64 `json:"bar"`
	// Stopped is whether the bound fell below the bar before the list's end.
	Stopped bool `json:"stopped"`
	Passed  bool `json:"passed"`
	// Reason says how the tier ended, naming the bound that stopped it.
	Reason string `json:"reason"`
}

// Screening returns the size of a list of n samples' screening fifth, ceil(n/5).
func Screening(n int) int { return (n + 4) / 5 }

// Run sends l's samples to judge in order: the screening fifth whole, then one at a time until
// the upper bound on the pass rate falls below bar or the list ends. A nil bar runs the list to
// its end and records it, never failing.
func Run(ctx context.Context, l List, bar *float64, judge Judge) (Result, error) {
	res := Result{Tier: l.Tier, Size: len(l.IDs), Screening: Screening(len(l.IDs)), Bar: bar}
	for i, id := range l.IDs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		ok, err := judge(ctx, id)
		if err != nil {
			return res, fmt.Errorf("tier %s: sample %s: %w", l.Tier, id, err)
		}
		res.Run = i + 1
		if ok {
			res.Passes++
		}
		res.Upper = UpperBound(res.Passes, res.Run)
		if bar != nil && res.Run >= res.Screening && res.Run < res.Size && res.Upper < *bar {
			res.Stopped = true
			break
		}
	}
	if res.Run > 0 {
		res.Rate = float64(res.Passes) / float64(res.Run)
	}
	switch {
	case bar == nil:
		res.Passed = true
		res.Reason = fmt.Sprintf("no bar: ran all %d, pass rate %.3f recorded", res.Run, res.Rate)
	case res.Stopped:
		res.Reason = fmt.Sprintf("stopped after %d of %d: the 95%% upper bound %.3f on the pass rate (%d/%d) fell below the bar %.3f",
			res.Run, res.Size, res.Upper, res.Passes, res.Run, *bar)
	default:
		res.Passed = res.Rate >= *bar
		verdict := "at or above"
		if !res.Passed {
			verdict = "below"
		}
		res.Reason = fmt.Sprintf("ran all %d: pass rate %.3f, %s the bar %.3f", res.Run, res.Rate, verdict, *bar)
	}
	return res, nil
}

// ReadListFile reads the list at path.
func ReadListFile(path string) (List, error) {
	f, err := os.Open(path)
	if err != nil {
		return List{}, err
	}
	defer func() { _ = f.Close() }()
	return ReadList(f)
}
