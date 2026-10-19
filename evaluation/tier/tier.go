package tier

import (
	"bufio"
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

// ReadListFile reads the list at path.
func ReadListFile(path string) (List, error) {
	f, err := os.Open(path)
	if err != nil {
		return List{}, err
	}
	defer func() { _ = f.Close() }()
	return ReadList(f)
}
