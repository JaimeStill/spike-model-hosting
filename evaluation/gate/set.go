package gate

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/concurrent"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// target is where a quality unit (an MTEB task, an ASR set, a vision or BFCL tier) runs: the
// model it runs on, the directory its output goes under, and whether its bar judges it and stops
// it early, as in the quality stage, or it runs whole and is recorded, as in the set stage. A
// full run's quality stage (full) continues the screen's quality stage (screen): it reuses each
// unit the screen ran whole and runs the rest of each one the screen stopped.
type target struct {
	model, dir string
	judged     bool
	full       bool
	screen     *StageResult
}

// unjudgedNote is the note a unit the set stage runs carries in place of its bar's.
const unjudgedNote = "set stage: run whole, recorded, never judged; the stage gates memory and crossing only"

// unjudged returns note for a judged target, and the set stage's note for one that isn't.
func (t target) unjudged(note string) string {
	if t.judged {
		return note
	}
	return unjudgedNote
}

// bar returns the bar named name for a judged target; an unjudged one has none.
func (r *Runner) bar(t target, name string) (tier.Bar, bool) {
	if !t.judged {
		return tier.Bar{}, false
	}
	return r.Bars.Bar(name)
}

// TierOne names the units of a role's tier 1, which the set stage runs, by check name: the agent
// role's BFCL single-turn questions; the vision role's probe image and V*Bench, its quality
// tier 1 whole; the audio role's first ASR set, LibriSpeech test-clean; and the embedding role's
// STSBenchmark, the first and cheapest of its MTEB tasks (15s on b11669, against Banking77's 57,
// NFCorpus's 72 and SciFact's 88), each task being run in full, so a task is its smallest unit.
func TierOne(role string) []string {
	switch role {
	case RoleAgent:
		return []string{AgentCheck(AgentTiers[0].Name)}
	case RoleVision:
		var out []string
		for _, t := range VisionTiers {
			if t.Tier == 1 {
				out = append(out, VisionCheck(t.Name))
			}
		}
		return out
	case RoleAudio:
		return []string{ASRTier(AudioSets[0])}
	case RoleEmbedding:
		return []string{EmbeddingTasks[0]}
	}
	return nil
}

// tierOne runs role's tier 1 on t's model, unit after unit, every unit whatever the one before
// it scored.
func (r *Runner) tierOne(ctx context.Context, t target, role string) []Check {
	switch role {
	case RoleAgent:
		return []Check{r.bfclTier(ctx, t, AgentTiers[0])}
	case RoleVision:
		var checks []Check
		for _, vt := range VisionTiers {
			if vt.Tier == 1 && ctx.Err() == nil {
				checks = append(checks, r.visionTier(ctx, t, vt))
			}
		}
		return checks
	case RoleAudio:
		return []Check{r.asrSet(ctx, t, AudioSets[0])}
	case RoleEmbedding:
		return []Check{r.mtebTask(ctx, t, EmbeddingTasks[0])}
	}
	return nil
}

// assignment is one role of a set and the model it assigns the role.
type assignment struct{ role, model string }

// assignments are the set's roles and their models, in the order Roles lists them.
func (r *Runner) assignments() []assignment {
	models := map[string]string{
		RoleAgent: r.Set.Roles.Chat, RoleVision: r.Set.Roles.Vision,
		RoleAudio: r.Set.Roles.Audio, RoleEmbedding: r.Set.Roles.Embedding,
	}
	var out []assignment
	for _, role := range Roles {
		if m := models[role]; m != "" {
			out = append(out, assignment{role, m})
		}
	}
	return out
}

// The set stage's phases, each the name of its directory under the run directory's set/.
const (
	phaseAlone      = "alone"
	phaseConcurrent = "concurrent"
)

// set runs the set stage against the set's loaded models, in a full run only: a screen's quality
// stage runs its tiers at once inside the same concurrent window, which stands in for the stage's
// concurrent phase. It takes one of two forms.
//
// A set run (no Model) runs each role's tier 1 (TierOne) on the model the set assigns it, first
// alone, one role after another, then every role's at once (setRun).
//
// A model run takes the alone figure for each of its roles from the run's quality stage, whose
// tier 1 runs the same list at the same settings (qualityTierOne), and runs only its own roles'
// tier 1s, all at once, while the set's other role models carry load for exactly that window
// (setModelRun).
//
// Either way the units run whole, without their bars, and their scores are recorded, never
// judged: a tier's bar judges a model alone, in its quality stage, and the set stage holds the
// set to its memory and crossing. The stage gates two things over the concurrent phase
// (window): the pool's least free memory, read as capacity reads it, against the pool's floor;
// and no crossed answer, from rounds of nonce requests sent to every role's model at once while
// the tiers run (crossing). A unit that can't run fails the stage too, as a measurement that
// couldn't be taken. Each unit's output is kept in the run directory's set/alone and
// set/concurrent.
func (r *Runner) set(ctx context.Context, rep *Report) []Check {
	roles := r.assignments()
	models := make([]string, len(roles))
	for i, a := range roles {
		models[i] = a.model
	}
	if err := router.CheckLoaded(ctx, r.HTTP, r.Router, models...); err != nil {
		return []Check{broken("set loaded", "", err)}
	}
	if r.Dir == "" {
		return []Check{broken("set loaded", "", errors.New("the runner has no run directory"))}
	}
	if r.Model == "" {
		return r.setRun(ctx, roles)
	}
	return r.setModelRun(ctx, rep, roles)
}

// targetOf is where a's tier 1 runs in the set stage's phase: on a's model, its output under
// the run directory's set/<phase>.
func (r *Runner) targetOf(a assignment, phase string) target {
	return target{model: a.model, dir: filepath.Join(r.Dir, "set", phase)}
}

// setRun is a set run's set stage: every role's tier 1 alone, role after role, then all at once.
func (r *Runner) setRun(ctx context.Context, roles []assignment) []Check {
	alone := map[string][]Check{}
	aloneFree := map[string]Pool{}
	for _, a := range roles {
		r.logf("evaluate: set: %s tier 1 alone on %s\n", a.role, a.model)
		low, readings, _ := r.during(ctx, func() { alone[a.role] = r.tierOne(ctx, r.targetOf(a, phaseAlone), a.role) })
		if readings > 0 {
			aloneFree[a.role] = low
		}
		if ctx.Err() != nil {
			var checks []Check
			for _, a := range roles {
				checks = append(checks, phased(alone[a.role], phaseAlone, nil)...)
			}
			return checks
		}
	}

	r.logf("evaluate: set: every role's tier 1 at once\n")
	together := map[string][]Check{}
	var mu sync.Mutex
	w := r.window(ctx, roles, func() {
		var tiers sync.WaitGroup
		for _, a := range roles {
			tiers.Go(func() {
				checks := r.tierOne(ctx, r.targetOf(a, phaseConcurrent), a.role)
				mu.Lock()
				together[a.role] = checks
				mu.Unlock()
			})
		}
		tiers.Wait()
	})

	var checks []Check
	for _, a := range roles {
		checks = append(checks, phased(alone[a.role], phaseAlone, nil)...)
		checks = append(checks, phased(together[a.role], phaseConcurrent, alone[a.role])...)
		ch := slowdown(a.role, alone[a.role], together[a.role], "")
		if p, ok := aloneFree[a.role]; ok {
			ch.Values["alone_pool_free_min_mib"] = float64(p.FreeMiB())
		}
		checks = append(checks, ch)
	}
	return append(checks, w.gates(r.Floors.Pool.FreeMiB, "every role's tier 1 ran at once")...)
}

// setModelRun is a model run's set stage. Its alone figures are the quality stage's tier 1 records,
// the same units on the same lists at the same settings, so it doesn't run them alone again: a
// model run's quality pass runs only the model's own roles, the set's other models idle but for the
// crossing rounds, and its full run's records are each list whole, the screen's samples and the
// rest together. Its own roles' tier 1s run at once while every other role of the set carries load
// (load): the load starts with the tiers and stops when they finish, so the other models load the
// pool for exactly the window the model's tiers run, and run no tier of their own.
func (r *Runner) setModelRun(ctx context.Context, rep *Report, roles []assignment) []Check {
	alone, err := qualityTierOne(rep.Stage(StageQuality), rep.Roles)
	if err != nil {
		return []Check{broken("quality tier 1 record", "", err)}
	}
	var own, others []assignment
	for _, a := range roles {
		if a.model == r.Model {
			own = append(own, a)
		} else {
			others = append(others, a)
		}
	}

	r.logf("evaluate: set: %s's tier 1 with the set's other roles under load\n", r.Model)
	c := &client{base: r.Router, hc: r.HTTP}
	loads := make([]*load, len(others))
	for i, a := range others {
		loads[i] = r.newLoad(c, a)
	}
	together := map[string][]Check{}
	var mu sync.Mutex
	w := r.window(ctx, roles, func() {
		loadCtx, stop := context.WithCancel(ctx)
		var bg sync.WaitGroup
		for _, l := range loads {
			bg.Go(func() { l.run(loadCtx) })
		}
		var tiers sync.WaitGroup
		for _, a := range own {
			tiers.Go(func() {
				checks := r.tierOne(ctx, r.targetOf(a, phaseConcurrent), a.role)
				mu.Lock()
				together[a.role] = checks
				mu.Unlock()
			})
		}
		tiers.Wait()
		stop()
		bg.Wait()
	})

	var checks []Check
	for _, a := range own {
		checks = append(checks, phased(together[a.role], phaseConcurrent, alone[a.role])...)
		checks = append(checks, slowdown(a.role, alone[a.role], together[a.role], "its quality tier 1"))
	}
	for _, l := range loads {
		checks = append(checks, l.check())
	}
	return append(checks, w.gates(r.Floors.Pool.FreeMiB, "the model's tier 1 ran with the set's other roles under load")...)
}

// qualityTierOne returns, by role, the quality stage's records of each of roles' tier 1 units
// (TierOne), in TierOne's order: the alone figures a model run's set stage reuses. It fails,
// naming the unit, when quality is missing or holds no passed record of a unit with a score and
// a wall time.
func qualityTierOne(quality *StageResult, roles []string) (map[string][]Check, error) {
	if quality == nil {
		return nil, errors.New("the run has no quality stage")
	}
	out := map[string][]Check{}
	for _, role := range roles {
		for _, unit := range TierOne(role) {
			i := slices.IndexFunc(quality.Checks, func(c Check) bool { return c.Role == role && c.Name == unit })
			if i < 0 {
				return nil, fmt.Errorf("the quality stage has no record of %s's %s", role, unit)
			}
			c := quality.Checks[i]
			if !c.Passed || c.Measured == nil || c.Values["wall_s"] <= 0 {
				return nil, fmt.Errorf("the quality stage's record of %s's %s has no passed score with a wall time", role, unit)
			}
			out[role] = append(out[role], c)
		}
	}
	return out, nil
}

// concurrentWindow is what the set stage's concurrent phase measured around its tiers: the
// pool's least free reading and the number of readings, the last reading's error, the crossing
// probe's rounds, and the phase's wall time.
type concurrentWindow struct {
	low      Pool
	readings int
	readErr  error
	cross    crossing
	wallS    float64
}

// window runs fn, the concurrent phase's tiers, while the footprint reads the pool and the
// crossing probe sends its rounds to every one of roles' models, and returns what they measured.
func (r *Runner) window(ctx context.Context, roles []assignment, fn func()) concurrentWindow {
	c := &client{base: r.Router, hc: r.HTTP}
	var w concurrentWindow
	began := time.Now()
	w.low, w.readings, w.readErr = r.during(ctx, func() {
		done := make(chan struct{})
		var probe sync.WaitGroup
		probe.Go(func() { w.cross = r.cross(ctx, c, roles, done) })
		fn()
		close(done)
		probe.Wait()
	})
	w.wallS = time.Since(began).Seconds()
	return w
}

// gates are the concurrent phase's two gates: no crossed answer, and the pool's least free
// memory against floor. what says what ran over the phase, for the pool's detail.
func (w concurrentWindow) gates(floor float64, what string) []Check {
	checks := []Check{w.cross.check()}
	if w.readings == 0 {
		return append(checks, broken("peak pool free", "", w.readErr))
	}
	peak := measure("peak pool free", "", float64(w.low.FreeMiB()), "MiB", AtLeast, floor)
	peak.Values = poolValues(w.low)
	peak.Values["readings"], peak.Values["phase_wall_s"] = float64(w.readings), w.wallS
	peak.Detail = fmt.Sprintf("%d of %d MiB used at the least free of %d readings over the %.0fs %s%s",
		w.low.UsedMiB, w.low.TotalMiB, w.readings, w.wallS, what, ramDetail(w.low))
	return append(checks, peak)
}

// loadInputs are an embedding load request's inputs: 64, the batch the MTEB encoder sends
// (mteb_quality.py's BATCH), capacity's sentences over and over.
var loadInputs = slices.Repeat(capacityInputs, 64/len(capacityInputs))

// load is one background role's work in a model run's set stage: workers, each sending the
// role's capacity request (recorder's for a generative role, an embedding batch of loadInputs
// for the embedding role) to the role's model, one after another, until stopped. It counts the
// requests answered and errored and the tokens they carried; a request cut off by the stop
// counts as neither.
type load struct {
	role, model string
	workers     int
	// send sends the n-th request and returns the tokens it carried: the predicted tokens of a
	// chat completion, the input tokens of an embedding batch.
	send func(ctx context.Context, n uint64) (int, error)

	mu                        sync.Mutex
	answered, errored, tokens int
	errs                      []string
	wall                      time.Duration
}

// newLoad returns a's load, sent by Runner.LoadWorkers workers (one when zero). The workers are a
// closed loop: each sends its next request as its last returns.
func (r *Runner) newLoad(c *client, a assignment) *load {
	l := &load{role: a.role, model: a.model, workers: max(r.LoadWorkers, 1)}
	if a.role == RoleEmbedding {
		l.send = func(ctx context.Context, _ uint64) (int, error) {
			got, err := c.embed(ctx, a.model, loadInputs)
			return got.PromptTokens, err
		}
		return l
	}
	rec := &recorder{c: c, model: a.model, role: a.role, media: r.Media, asr: r.asrPrompt(a.model)}
	l.send = func(ctx context.Context, n uint64) (int, error) {
		comp, err := c.complete(ctx, rec.body(concurrent.Prompt(fmt.Sprintf("nonce-%016x", n))))
		return comp.Timings.PredictedN, err
	}
	return l
}

// loadPause is how long a load worker waits after an errored request before its next, so a
// model that refuses requests isn't sent them in a tight loop.
const loadPause = time.Second

// run runs the load's workers until ctx is done.
func (l *load) run(ctx context.Context) {
	began := time.Now()
	var n atomic.Uint64
	var wg sync.WaitGroup
	for range l.workers {
		wg.Go(func() {
			for ctx.Err() == nil {
				tokens, err := l.send(ctx, n.Add(1))
				l.mu.Lock()
				switch {
				case ctx.Err() != nil:
				case err != nil:
					l.errored++
					l.errs = append(l.errs, err.Error())
				default:
					l.answered++
					l.tokens += tokens
				}
				l.mu.Unlock()
				if err != nil {
					select {
					case <-ctx.Done():
					case <-time.After(loadPause):
					}
				}
			}
		})
	}
	wg.Wait()
	l.wall = time.Since(began)
}

// check records the load: its requests answered a second over the window, beside the requests
// answered and errored, the workers, the window and the tokens a second. It is recorded, never
// judged; but a load whose every finished request errored put nothing on its model, and fails
// the stage as a measurement that couldn't be taken. (A window shorter than one request ends with
// none finished and none errored: its requests were in flight throughout.)
func (l *load) check() Check {
	window := l.wall.Seconds()
	tokens := "predicted_tok_s"
	if l.role == RoleEmbedding {
		tokens = "input_tok_s"
	}
	values := map[string]float64{
		"requests": float64(l.answered), "errors": float64(l.errored), "workers": float64(l.workers), "window_s": window,
	}
	if l.answered == 0 && l.errored > 0 {
		ch := broken("load", l.role, fmt.Errorf("none of %s's load requests was answered, %d errored: %s", l.model, l.errored, listed(l.errs)))
		ch.Values = values
		return ch
	}
	rate, perS := 0.0, 0.0
	if window > 0 {
		rate, perS = float64(l.answered)/window, float64(l.tokens)/window
	}
	values[tokens] = perS
	ch := Check{Name: "load", Role: l.role, Measured: &rate, Unit: "req/s", Passed: true, Values: values}
	notes := []string{fmt.Sprintf("%d workers sending capacity's %s request to %s one after another over the %.0fs the model's tier 1 ran: %d answered, %d errored",
		l.workers, l.role, l.model, window, l.answered, l.errored)}
	if len(l.errs) > 0 {
		notes = append(notes, "errors: "+listed(l.errs))
	}
	ch.Detail = strings.Join(append(notes, "background load, recorded, never judged"), "; ")
	return ch
}

// phased returns checks renamed for the phase, "<unit> <phase>". A concurrent unit also records
// its alone counterpart's score and wall time, its score's change from it and its slowdown, its
// wall time over the alone one's.
func phased(checks []Check, phase string, alone []Check) []Check {
	out := make([]Check, len(checks))
	for i, c := range checks {
		c.Name = c.Name + " " + phase
		values := map[string]float64{}
		maps.Copy(values, c.Values)
		c.Values = values
		for _, a := range alone {
			if a.Name != checks[i].Name {
				continue
			}
			if a.Measured != nil && c.Measured != nil {
				values["alone_score"] = *a.Measured
				values["score_change"] = *c.Measured - *a.Measured
			}
			if aw, cw := a.Values["wall_s"], c.Values["wall_s"]; aw > 0 && cw > 0 {
				values["alone_wall_s"] = aw
				values["slowdown"] = cw / aw
			}
		}
		out[i] = c
	}
	return out
}

// slowdown is a role's tier 1 wall time at once over its wall time alone, each the sum of its
// units', recorded, never judged. source, when set, names where the alone figure came from.
func slowdown(role string, alone, together []Check, source string) Check {
	wall := func(checks []Check) float64 {
		s := 0.0
		for _, c := range checks {
			s += c.Values["wall_s"]
		}
		return s
	}
	a, t := wall(alone), wall(together)
	ch := Check{Name: "tier 1 slowdown", Role: role, Unit: "x", Passed: true,
		Values: map[string]float64{"alone_wall_s": a, "concurrent_wall_s": t}}
	if a > 0 && t > 0 {
		v := t / a
		ch.Measured = &v
	}
	if source != "" {
		source = ", " + source
	}
	ch.Detail = fmt.Sprintf("%s: %.0fs at once against %.0fs alone%s; recorded, never judged", strings.Join(TierOne(role), " + "), t, a, source)
	return ch
}

// crossEvery spaces the crossing probe's rounds when Runner.CrossEvery is zero: a round each
// 15s adds one request a model to the tiers' four in flight, a small share of their load, and
// still sends some forty a role over a ten-minute phase.
const crossEvery = 15 * time.Second

// crossing is what the crossing probe saw: its rounds; each role's requests the probe could
// check, answered and so checkable; the requests that went unanswered, each naming its error;
// and the answers that crossed, each naming its request.
type crossing struct {
	rounds     int
	roles      []string
	checked    map[string]int
	unanswered []string
	crossed    []string
}

// cross sends rounds of nonce requests, one to each role's model at once, until done closes: a
// round runs at once and checks itself through the concurrent package, so every answer must
// carry its own nonce and no other round-mate's, from another role's model or its own; every
// embedding vector must match its own input's lone embedding within the embedding tolerance.
// Each generative role's request is capacity's for the role (recorder): the agent's text, the
// vision model's image and the audio model's clip, each asking for the nonce back. Since the
// requests go to each model beside its tier's own, an answer that carried a tier's content in
// place of its own nonce fails too, as would one that went to another model. The first round
// starts with the tiers; a round in flight when they finish runs to its end.
//
// A request that errored, came back empty, or hit its max_tokens carrying no nonce at all
// (finish_reason "length") carries nothing to cross, so it is recorded unanswered rather than
// crossed: such an answer is a reliability fault the stage notes, not another request's content.
// Only an answer carrying a nonce not its own is crossed, cut off or not. (On set A, Gemma 4
// 26B-A4B's three short or empty answers in the 2026-10-17 baseline each generated exactly the
// vision request's then 512 tokens, by the router's log.) The check still needs each role
// checked at least once.
func (r *Runner) cross(ctx context.Context, c *client, roles []assignment, done <-chan struct{}) crossing {
	every := r.CrossEvery
	if every == 0 {
		every = crossEvery
	}
	cfg := concurrent.Config{Rounds: 1, RequestTimeout: requestTimeout}
	res := crossing{checked: map[string]int{}}
	for _, a := range roles {
		res.roles = append(res.roles, a.role)
		if a.role == RoleEmbedding {
			_, document := r.embedPrompts(a.model)
			cfg.Embed = append(cfg.Embed, concurrent.EmbedStream{Name: a.role, Embedder: embedder{c, a.model, document}, Inputs: capacityInputs})
			continue
		}
		cfg.Chat = append(cfg.Chat, concurrent.ChatStream{
			Name: a.role, Chatter: &recorder{c: c, model: a.model, role: a.role, media: r.Media, asr: r.asrPrompt(a.model), cutOff: true}, Workers: 1,
		})
	}
	for {
		rep, err := concurrent.Run(ctx, cfg)
		res.rounds++
		if err != nil {
			res.unanswered = append(res.unanswered, fmt.Sprintf("round %d: %v", res.rounds, err))
		}
		res.add(rep)
		select {
		case <-done:
			return res
		case <-ctx.Done():
			return res
		case <-time.After(every):
		}
	}
}

// add sorts a round's requests into checked, unanswered and crossed.
func (x *crossing) add(rep concurrent.Report) {
	var crossed concurrent.Report
	for _, c := range rep.Chats {
		switch {
		case c.Err != nil:
			x.unanswered = append(x.unanswered, fmt.Sprintf("round %d: %s: %v", x.rounds, c.ID, c.Err))
			continue
		case !c.Passed():
			crossed.Chats = append(crossed.Chats, c)
		}
		x.checked[c.Stream]++
	}
	for _, e := range rep.Embeds {
		switch {
		case e.Err != nil:
			x.unanswered = append(x.unanswered, fmt.Sprintf("round %d: %s: %v", x.rounds, e.ID, e.Err))
			continue
		case !e.Passed():
			crossed.Embeds = append(crossed.Embeds, e)
		}
		x.checked[e.Stream]++
	}
	for _, f := range crossed.Failures() {
		x.crossed = append(x.crossed, fmt.Sprintf("round %d: %s", x.rounds, f))
	}
}

// maxListed caps the requests a crossing check's detail lists of each kind.
const maxListed = 5

// listed joins up to maxListed of lines, saying how many more there were.
func listed(lines []string) string {
	s := strings.Join(lines[:min(len(lines), maxListed)], "; ")
	if n := len(lines) - maxListed; n > 0 {
		s += fmt.Sprintf("; and %d more", n)
	}
	return s
}

// check is the crossing probe's check: passed when no answer or vector crossed and every role
// had at least one checked.
func (x crossing) check() Check {
	var counts, unchecked []string
	for _, role := range x.roles {
		counts = append(counts, fmt.Sprintf("%s %d", role, x.checked[role]))
		if x.checked[role] == 0 {
			unchecked = append(unchecked, role)
		}
	}
	notes := []string{fmt.Sprintf("%d rounds while the tiers ran; checked: %s (answers, or embedding vectors)", x.rounds, strings.Join(counts, ", "))}
	switch {
	case len(x.crossed) > 0:
		notes = append(notes, fmt.Sprintf("%d crossed: %s", len(x.crossed), listed(x.crossed)))
	case len(unchecked) > 0:
		notes = append(notes, "nothing checked for "+strings.Join(unchecked, ", "))
	default:
		notes = append(notes, "each answer carried its own nonce and no other, each vector was its own input's")
	}
	if len(x.unanswered) > 0 {
		notes = append(notes, fmt.Sprintf("%d unanswered, recorded, not crossed: %s", len(x.unanswered), listed(x.unanswered)))
	}
	ch := fact("no crossed answer", "", strings.Join(notes, "; "), len(x.crossed) == 0 && len(unchecked) == 0)
	ch.Values = map[string]float64{"rounds": float64(x.rounds), "crossed": float64(len(x.crossed)), "unanswered": float64(len(x.unanswered))}
	for _, role := range x.roles {
		ch.Values["checked_"+role] = float64(x.checked[role])
	}
	return ch
}
