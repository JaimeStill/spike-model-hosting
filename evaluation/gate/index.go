package gate

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// IndexFile is the results index's file name under the state root's evaluate/, beside the run
// directories: <state>/evaluate/index.json.
const IndexFile = "index.json"

// ModelKey is what a model's results are kept by: the model, the router's build (its /props
// build_info), and the SHA-256 of the preset section it runs at (profile.Profile.PresetSection),
// with the SHA-256 of the client settings the profile gives it beside them, its card's prompts
// and its consumer reasoning, which render into no preset but change what the clients send it.
type ModelKey struct {
	Model   string `json:"model"`
	Build   string `json:"build"`
	Section string `json:"section_sha256"`
	Clients string `json:"clients_sha256"`
}

// digest is the key's own SHA-256, what an index entry holds.
func (k ModelKey) digest() string {
	b, err := json.Marshal(k)
	if err != nil {
		panic(err) // a struct of strings always marshals
	}
	return sha256Hex(b)
}

// Keys are a run's keys: each of the set's models' (ModelKey), the set's, a SHA-256 over the
// set's role assignments and every one of its models' keys, so a change to any model's key
// changes it, and the method's, a SHA-256 over what judges and drives every stage (the floors,
// the bars, the suites' pins, the tier lists and the probe's media), which an entry must match to
// be reused at all.
type Keys struct {
	Models []ModelKey `json:"models"`
	Set    string     `json:"set_sha256"`
	Method string     `json:"method_sha256"`
}

// model returns id's key digest, or "" for a model not of the set, or no keys.
func (k *Keys) model(id string) string {
	if k == nil {
		return ""
	}
	for _, m := range k.Models {
		if m.Model == id {
			return m.digest()
		}
	}
	return ""
}

// Entry is one passed result the index keeps: a stage's record of one unit, under its key. A
// model's units are fitness's and capacity's records of one role of the model (Role, no Unit) and
// the quality stage's record of one of the role's units (Unit, a check name: an MTEB task, an ASR
// set's tier, a vision or BFCL tier), each under the model's key; the conform tier's records
// (agent-conform and each harness's) are one unit under the set's key, since harness fit sends to
// every role's model of the set. The set stage's record of a run (Subject, the model or the set
// run's SetSubject) is one unit under the set's key. Checks are a copy of the records, so an
// entry stays reusable once its run directory is removed.
type Entry struct {
	Stage   string `json:"stage"`
	Role    string `json:"role,omitempty"`
	Unit    string `json:"unit,omitempty"`
	Subject string `json:"subject,omitempty"`
	// Model is the model whose key the entry is under, empty for one under the set's.
	Model string `json:"model,omitempty"`
	// Build is the router's build the result was measured on, which the key holds too.
	Build  string `json:"build"`
	Key    string `json:"key"`
	Method string `json:"method"`
	// Mode is the mode of the run that measured it, and Run that run's directory name, under
	// <state>/evaluate/.
	Mode   string  `json:"mode"`
	Run    string  `json:"run"`
	Checks []Check `json:"checks"`
}

// same reports whether e and o are the same unit under the same key and method, of the same mode.
func (e Entry) same(o Entry) bool {
	return e.Stage == o.Stage && e.Role == o.Role && e.Unit == o.Unit && e.Subject == o.Subject &&
		e.Key == o.Key && e.Method == o.Method && e.Mode == o.Mode
}

// Index is evaluate's index of its passed results, kept as JSON at Path, which LoadIndex reads and
// Save writes. A run with an Index reuses each unit whose key matches a passed entry and records
// each unit it passes; the caller saves the index after the run, as it writes the report.
type Index struct {
	Path    string  `json:"-"`
	Entries []Entry `json:"entries"`
}

// LoadIndex reads the index under the state root, <state>/evaluate/IndexFile, or returns an empty
// one there when there is none yet.
func LoadIndex(state string) (*Index, error) {
	x := &Index{Path: filepath.Join(state, "evaluate", IndexFile)}
	b, err := os.ReadFile(x.Path)
	if errors.Is(err, os.ErrNotExist) {
		return x, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, x); err != nil {
		return nil, fmt.Errorf("%s: %w", x.Path, err)
	}
	return x, nil
}

// Save writes the index to Path, through a file beside it renamed into place, so a run cut off
// mid-write leaves the last index whole.
func (x *Index) Save() error {
	if err := os.MkdirAll(filepath.Dir(x.Path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		return err
	}
	tmp := x.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, x.Path)
}

// find returns the entry a run of mode may reuse for want's unit under want's key and method: a
// full run's for a full run, which judges each tier's whole list; a full run's or else a
// screen's for a screen, the newest of each (run directories are named by their UTC start).
func (x *Index) find(mode string, want Entry) (Entry, bool) {
	modes := []string{ModeFull}
	if mode == ModeScreen {
		modes = append(modes, ModeScreen)
	}
	for _, m := range modes {
		want.Mode = m
		var found []Entry
		for _, e := range x.Entries {
			if e.same(want) {
				found = append(found, e)
			}
		}
		if len(found) > 0 {
			return slices.MaxFunc(found, func(a, b Entry) int { return cmp.Compare(a.Run, b.Run) }), true
		}
	}
	return Entry{}, false
}

// put keeps e, in place of the index's entry of the same unit, key, method and mode.
func (x *Index) put(e Entry) {
	x.Entries = slices.DeleteFunc(x.Entries, e.same)
	x.Entries = append(x.Entries, e)
}

// keysOf returns the run's keys on build: each of the set's models' key, the set's and the
// method's.
func (r *Runner) keysOf(build string) (*Keys, error) {
	k := &Keys{}
	var set strings.Builder
	for _, a := range r.assignments() {
		set.WriteString(a.role + "=" + a.model + "\n")
	}
	for _, id := range slices.Sorted(slices.Values(r.Set.Models)) {
		section, err := r.Profile.PresetSection(id)
		if err != nil {
			return nil, err
		}
		// The client settings are what the clients send, without their sources and reasons.
		m := r.Profile.Model(id)
		var prompts any
		if p := m.Prompts; p != nil {
			prompts = []any{p.Embedding, p.ASR}
		}
		clients, err := json.Marshal([]any{prompts, m.Consumers.Pi, m.Consumers.OpenCode})
		if err != nil {
			return nil, err
		}
		mk := ModelKey{Model: id, Build: build, Section: sha256Hex(section), Clients: sha256Hex(clients)}
		k.Models = append(k.Models, mk)
		set.WriteString(mk.digest() + "\n")
	}
	k.Set = sha256Hex([]byte(set.String()))
	method, err := r.method()
	if err != nil {
		return nil, err
	}
	k.Method = method
	return k, nil
}

// method returns the SHA-256 over what judges and drives the stages: the floors, the bars, the
// suites' pins, every tier list in Tiers and the probe's media. A change to any of them makes
// every result measured before it unreusable.
func (r *Runner) method() (string, error) {
	tiers := map[string]string{}
	if r.Tiers != "" {
		paths, err := filepath.Glob(filepath.Join(r.Tiers, "*.txt"))
		if err != nil {
			return "", err
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			tiers[filepath.Base(p)] = sha256Hex(b)
		}
	}
	// encoding/json writes a map's keys sorted, so the same method marshals the same.
	b, err := json.Marshal(map[string]any{
		"floors": r.Floors, "bars": r.Bars, "tiers": tiers,
		"pins":  map[string]string{"mteb": MTEBVersion, "inspect-ai": InspectVersion, "inspect-evals": InspectEvalsVersion},
		"media": map[string]string{"png": sha256Hex(r.Media.PNG), "wav": sha256Hex(r.Media.WAV)},
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}

// sha256Hex is b's SHA-256, in hex.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// modelEntry is the identity of model's unit of stage for role, under the model's key.
func (r *Runner) modelEntry(stage, role, unit, model string) Entry {
	return Entry{Stage: stage, Role: role, Unit: unit, Model: model, Key: r.keys.model(model)}
}

// setEntry is the identity of a unit of stage under the set's key: the conform tier's, or, for
// subject, the set stage's.
func (r *Runner) setEntry(stage, role, unit, subject string) Entry {
	e := Entry{Stage: stage, Role: role, Unit: unit, Subject: subject}
	if r.keys != nil {
		e.Key = r.keys.Set
	}
	return e
}

// reusable returns the index's passed record of want's unit that a run of mode may reuse, each
// check marked with the run it was measured in (Check.ReusedFrom), and false when the run reuses
// nothing (no Index, or Fresh) or the index holds no such record.
func (r *Runner) reusable(mode string, want Entry) ([]Check, bool) {
	if r.Index == nil || r.Fresh || r.keys == nil || want.Key == "" {
		return nil, false
	}
	want.Method = r.keys.Method
	e, ok := r.Index.find(mode, want)
	if !ok {
		return nil, false
	}
	checks := slices.Clone(e.Checks)
	for i := range checks {
		checks[i].ReusedFrom = e.Run
	}
	r.logf("evaluate: %s reused from %s\n", strings.Join(slices.DeleteFunc([]string{e.Stage, e.Model, e.Subject, e.Role, e.Unit}, func(s string) bool { return s == "" }), " "), e.Run)
	return checks, true
}

// unit returns role's quality unit named name on t's model: the index's passed record of it,
// when the run may reuse one, or else run's checks. The conform tier's record is under the set's
// key, every other unit's under its model's.
func (r *Runner) unit(t target, role, name string, run func() []Check) []Check {
	mode := ModeScreen
	if t.full {
		mode = ModeFull
	}
	want := r.modelEntry(StageQuality, role, name, t.model)
	if name == AgentCheck(ConformTier) {
		want = r.setEntry(StageQuality, role, name, "")
	}
	if checks, ok := r.reusable(mode, want); ok {
		return checks
	}
	return run()
}

// unitOf names the quality unit a check is a record of: its own name, or, for a harness's fit,
// the conform tier's.
func unitOf(name string) string {
	conform := AgentCheck(ConformTier)
	if strings.HasPrefix(name, conform+"-") {
		return conform
	}
	return name
}

// record keeps rep's passed results in the index, each a copy of its records: each role's
// fitness and capacity records of each model, every check of the role passed; each quality unit
// whose checks all passed; and a passed set stage. A stage the run took from the run it resumed
// is left as that run recorded it, as is a screen's record it reused; a full run records each
// unit it judged whole under its own mode, from the run that measured it. The pool's gates over
// the whole set (fitness's warmed peak, the quality pass's two gates) judge no role and are never
// kept: they are measured in every run. A run with no Index or no run directory records nothing.
func (r *Runner) record(rep *Report) {
	if r.Index == nil || r.keys == nil || r.Dir == "" {
		return
	}
	run := filepath.Base(r.Dir)
	put := func(e Entry, checks []Check) {
		e.Method, e.Build, e.Mode, e.Run = r.keys.Method, rep.Build, rep.Mode, run
		for _, c := range checks {
			if c.ReusedFrom != "" {
				e.Run = c.ReusedFrom
			}
			c.ReusedFrom, c.Model = "", ""
			e.Checks = append(e.Checks, c)
		}
		r.Index.put(e)
	}
	roleModel := map[string]string{}
	for _, a := range r.assignments() {
		roleModel[a.role] = a.model
	}
	for _, s := range rep.Stages {
		if s.Reused || (s.Status != StatusPass && s.Status != StatusFail) {
			continue
		}
		switch s.Name {
		case StageFitness, StageCapacity:
			for _, g := range groups(s.Checks, func(c Check) string { return cmp.Or(c.Model, r.Model) + "\x00" + c.Role }) {
				c := g[0]
				if c.Role != "" && passed(g) && !reusedAny(g) {
					put(r.modelEntry(s.Name, c.Role, "", cmp.Or(c.Model, r.Model)), g)
				}
			}
		case StageQuality:
			for _, g := range groups(s.Checks, func(c Check) string { return c.Role + "\x00" + unitOf(c.Name) }) {
				c := g[0]
				if c.Role == "" || !passed(g) || (rep.Mode == ModeScreen && reusedAny(g)) {
					continue
				}
				name := unitOf(c.Name)
				want := r.modelEntry(StageQuality, c.Role, name, roleModel[c.Role])
				if name == AgentCheck(ConformTier) {
					want = r.setEntry(StageQuality, c.Role, name, "")
				}
				if want.Key != "" {
					put(want, g)
				}
			}
		case StageSet:
			if s.Status == StatusPass && !reusedAny(s.Checks) {
				put(r.setEntry(StageSet, "", "", cmp.Or(r.Model, SetSubject(r.Set.Name))), s.Checks)
			}
		}
	}
}

// groups splits checks by key, each group in the checks' order, the groups in their first
// check's.
func groups(checks []Check, key func(Check) string) [][]Check {
	var order []string
	by := map[string][]Check{}
	for _, c := range checks {
		k := key(c)
		if _, ok := by[k]; !ok {
			order = append(order, k)
		}
		by[k] = append(by[k], c)
	}
	out := make([][]Check, len(order))
	for i, k := range order {
		out[i] = by[k]
	}
	return out
}

// passed reports whether every check passed.
func passed(checks []Check) bool {
	return !slices.ContainsFunc(checks, func(c Check) bool { return !c.Passed })
}

// reusedAny reports whether any check was reused from the index.
func reusedAny(checks []Check) bool {
	return slices.ContainsFunc(checks, func(c Check) bool { return c.ReusedFrom != "" })
}

// screenDir returns the directory, rel under a run directory, holding the screen's output of a
// unit a full run continues: this run's own, or, for a record the screen reused from the index
// (screen.ReusedFrom), that of the run that measured it, beside this one under the state root's
// evaluate/. It fails when that run's directory is gone, as once its findings are committed.
func (t target) screenDir(screen Check, rel string) (string, error) {
	if screen.ReusedFrom == "" {
		return filepath.Join(t.dir, rel), nil
	}
	dir := filepath.Join(filepath.Dir(t.dir), screen.ReusedFrom, rel)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("the screen reused its record of %s from run %s, whose output the rest continues is gone (%w); screen again with -fresh", screen.Name, screen.ReusedFrom, err)
	}
	return dir, nil
}
