package gate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// SetSweep sweeps every model of a set, one after another in role order (SweepOrder), and
// commits each one's chosen slots and c before the next is swept: so each model is swept against
// the settings already chosen for the models before it, and at their committed settings for the
// models after it.
type SetSweep struct {
	// Runner is the set's run, as a Sweep's Runner is a model's, with no Model: its router, its
	// committed profile and set, floors, footprint, media, warm-up's peak finder and tier lists.
	// Each model's sweep runs on a copy of it with the model named and the profile as committed
	// so far.
	Runner *Runner
	// Presets is the router's presets file; Targets, the roles' per-session targets.
	Presets Presets
	Targets Targets
	// Profile is the committed profile's file, which each model's chosen setting is written into.
	Profile ProfileFile
	// DirOf is a model's sweep's run directory, where its report is written once it is swept and
	// its setting installed.
	DirOf func(model string) string
	// From, when set, is the role whose model the set sweep starts at: the models before it in
	// SweepOrder keep their committed settings, and are not swept again.
	From string
	// RestoreTimeout bounds each restore and install; zero is 15 minutes.
	RestoreTimeout time.Duration
}

// ProfileFile is the committed profile on disk, which a set sweep writes each chosen setting
// into. It must be safe for concurrent use.
type ProfileFile interface {
	// Commit writes model's setting into the file and returns the profile as the file now reads.
	Commit(ctx context.Context, model string, s profile.Setting) (*profile.Profile, error)
}

// FileProfile is the profile's TOML file: Commit edits its four keys in place
// (profile.SetSetting), every other byte as it was, writes the result beside the file and renames
// it over it, keeping the file's mode.
type FileProfile struct {
	Path string
}

// Commit writes model's setting into the file (FileProfile).
func (f FileProfile) Commit(_ context.Context, model string, s profile.Setting) (*profile.Profile, error) {
	src, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(f.Path)
	if err != nil {
		return nil, err
	}
	out, err := profile.SetSetting(src, model, s)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), "."+filepath.Base(f.Path)+".*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), f.Path); err != nil {
		return nil, err
	}
	return profile.Load(f.Path)
}

// Swept is one model of a set sweep: the models SweepOrder lists, with the roles the set assigns
// each.
type Swept struct {
	Model string
	Roles []string
}

// SweepOrder lists set's models in the order a set sweep sweeps them: by role, agent, vision,
// audio and embedding, each model once, at the first of its roles, with every role the set
// assigns it. A model the set assigns two roles is swept once, at the larger of their targets.
func SweepOrder(set *profile.Set) []Swept {
	r := &Runner{Set: set}
	var out []Swept
	for _, a := range r.assignments() {
		if i := slices.IndexFunc(out, func(s Swept) bool { return s.Model == a.model }); i >= 0 {
			out[i].Roles = append(out[i].Roles, a.role)
			continue
		}
		out = append(out, Swept{Model: a.model, Roles: []string{a.role}})
	}
	return out
}

// Run sweeps each model of the set in SweepOrder, from From's model when From is set. Each model's
// sweep is a Sweep.Run, which restores the render committed before it; once it chooses, the set
// sweep writes the chosen N and its step's c into the profile (Profile.Commit), with reasons that
// cite the sweep (its N, bound, build and date), and installs the new render as the committed
// one (Sweep.Restore with the new profile): checked by its SHA-256 on the host, the router's
// reload, and every model of the set serving at its committed setting. The next model's sweep
// starts from that render. The install runs on a context of its own, so an interrupt after the
// profile is written still installs the render it now commits.
//
// Run stops at the first model whose sweep fails, chooses no N (no step held, the pool's floor
// among the bounds), or whose commit or install fails; the models before it stay committed and
// installed. It returns each model's report, its Install set once installed, in the order swept;
// each is written to its DirOf directory as it ends.
func (s *SetSweep) Run(ctx context.Context) ([]*SweepReport, error) {
	base := s.Runner
	order := SweepOrder(base.Set)
	if s.From != "" {
		i := slices.IndexFunc(order, func(sw Swept) bool { return slices.Contains(sw.Roles, s.From) })
		if i < 0 {
			return nil, fmt.Errorf("set %s assigns no model the %s role", base.Set.Name, s.From)
		}
		order = order[i:]
	}
	prof, set := base.Profile, base.Set
	var reps []*SweepReport
	for _, sw := range order {
		if err := ctx.Err(); err != nil {
			return reps, err
		}
		r := *base
		r.Profile, r.Set, r.Model, r.Dir = prof, set, sw.Model, s.DirOf(sw.Model)
		sweep := &Sweep{Runner: &r, Presets: s.Presets, Targets: s.Targets, RestoreTimeout: s.RestoreTimeout}
		rep, err := sweep.Run(ctx)
		if rep == nil {
			return reps, err
		}
		reps = append(reps, rep)
		if err == nil && rep.Chosen == 0 {
			err = fmt.Errorf("no step of %s's sweep held (bound by %s): the set sweep stops, %s at its committed setting",
				sw.Model, cmp.Or(strings.Join(rep.Bounds, ", "), "none"), sw.Model)
		}
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			prof, set, err = s.commit(ctx, prof, rep)
		}
		if werr := WriteSweep(r.Dir, rep); werr != nil {
			err = errors.Join(err, werr)
		}
		if err != nil {
			return reps, err
		}
	}
	return reps, nil
}

// commit writes rep's chosen setting into the profile, cur as committed before it, and installs
// the profile's render as the committed one, recording the install in rep.Install; it returns
// the profile and set as now committed.
func (s *SetSweep) commit(ctx context.Context, cur *profile.Profile, rep *SweepReport) (*profile.Profile, *profile.Set, error) {
	st := chosenStep(rep)
	m := cur.Model(rep.Model)
	if st == nil || m == nil {
		return nil, nil, fmt.Errorf("%s's sweep chose N = %d but holds no step at it", rep.Model, rep.Chosen)
	}
	sweptSlots, sweptC := SweepReasons(rep, m.Kind == profile.KindEmbedding)
	slotsReason, cReason := WithSweep(m.SlotsReason, sweptSlots), WithSweep(m.CReason, sweptC)
	prof, err := s.Profile.Commit(ctx, rep.Model, profile.Setting{Slots: st.N, C: st.C, SlotsReason: slotsReason, CReason: cReason})
	if err != nil {
		return nil, nil, fmt.Errorf("committing %s's setting to the profile: %w", rep.Model, err)
	}
	set, err := prof.Set(rep.Set)
	if err != nil {
		return nil, nil, err
	}
	r := *s.Runner
	r.Profile, r.Set, r.Model = prof, set, rep.Model
	install := &Sweep{Runner: &r, Presets: s.Presets}
	ictx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cmp.Or(s.RestoreTimeout, defaultRestoreTimeout))
	defer cancel()
	r.logf("evaluate: install the profile with %s at %d slots, c = %d\n", rep.Model, st.N, st.C)
	res, err := install.Restore(ictx)
	rep.Install = res
	return prof, set, err
}

// chosenStep is rep's step at its chosen N, nil when it has none.
func chosenStep(rep *SweepReport) *SweepStep {
	for i := range rep.Steps {
		if st := &rep.Steps[i]; st.N == rep.Chosen && st.Held {
			return st
		}
	}
	return nil
}

// boundReasons say why a sweep's chosen N is no larger, for each bound.
var boundReasons = map[string]string{
	BoundFloor:   "the next step failed a floor at N",
	BoundPool:    "the next step left the pool under its floor",
	BoundContext: "the next step's sessions ran out of context or past the card's",
	BoundKnee:    "past it total throughput gains under 10% a step (the knee)",
	BoundTop:     "it is the sweep's last step",
}

// SweptMark begins the paragraph a set sweep writes into a model's slots_reason and c_reason,
// after the reason's own paragraphs, the model's facts: a later sweep replaces it (WithSweep), so
// a reason carries the last sweep alone.
const SweptMark = "Swept: "

// WithSweep is reason with its paragraph from a sweep, swept, in place of any it held: reason's
// other paragraphs, split at blank lines, kept as they are and in order, then swept.
func WithSweep(reason, swept string) string {
	var out []string
	for para := range strings.SplitSeq(strings.TrimSpace(reason), "\n\n") {
		if p := strings.TrimSpace(para); p != "" && !strings.HasPrefix(p, SweptMark) {
			out = append(out, p)
		}
	}
	return strings.Join(append(out, swept), "\n\n")
}

// SweepReasons are the paragraphs a set sweep adds to a model's slots_reason and c_reason, each
// beginning SweptMark and citing the sweep: its chosen N and what bound it, the build and the
// date; embedding says the model is an embedding model, whose c the sweep holds.
func SweepReasons(rep *SweepReport, embedding bool) (slots, c string) {
	var why []string
	for _, b := range rep.Bounds {
		why = append(why, boundReasons[b])
	}
	knee := "no knee"
	if rep.Knee > 0 {
		knee = fmt.Sprintf("the knee at %d", rep.Knee)
	}
	date := rep.Started.UTC().Format(time.DateOnly)
	slots = fmt.Sprintf("%s%d slots, chosen by evaluate sweep on %s, %s, in set %s beside the settings chosen before it: the smaller of the largest N that held every floor at N, the pool floor and every session's context (%d) and %s. Bound by %s: %s. The sweep's table is in docs/findings/slots.md.",
		SweptMark, rep.Chosen, rep.Build, date, rep.Set, rep.Held, knee, strings.Join(rep.Bounds, " and "), strings.Join(why, "; "))
	st := chosenStep(rep)
	switch {
	case st == nil:
	case embedding:
		c = fmt.Sprintf("%sc = %d held at its committed value by evaluate sweep on %s, %s, which swept the slots alone: llama.cpp keeps no KV memory for an embedding model, so c bounds each input, %d tokens at the %s role's target (evaluation/targets.toml).",
			SweptMark, st.C, rep.Build, date, rep.Target, rep.TargetRole)
	default:
		c = fmt.Sprintf("%sc = %d is %d sessions of the %s role's per-session target, %d tokens (evaluation/targets.toml), rounded up to a multiple of 256: the context evaluate sweep's step at %d ran, on %s, %s.",
			SweptMark, st.C, st.N, rep.TargetRole, rep.Target, st.N, rep.Build, date)
	}
	return slots, c
}

// WriteSetSweepSummary writes a set sweep for a reader: each model's summary in the order swept,
// then a line a model with its chosen setting and bounds.
func WriteSetSweepSummary(w io.Writer, set string, reps []*SweepReport) error {
	for _, rep := range reps {
		if err := WriteSweepSummary(w, rep); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "set %s swept, in role order:\n", set)
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "model\troles\tchosen N\tc\tbound by\tinstalled\t")
	for _, rep := range reps {
		c, installed := "-", "no"
		if st := chosenStep(rep); st != nil {
			c = fmt.Sprint(st.C)
		}
		if rep.Install != nil && rep.Install.Verified {
			installed = "sha256 " + rep.Install.SHA256
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t\n", rep.Model, strings.Join(rep.Roles, ", "), rep.Chosen, c,
			cmp.Or(strings.Join(rep.Bounds, ", "), "-"), installed)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}
