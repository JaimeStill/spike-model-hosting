package gate

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/concurrent"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// target is where a quality unit (an MTEB task, an ASR set, a vision or BFCL tier) runs: the
// model it runs on, the directory its output goes under, and whether its bar judges it and stops
// it early, as in the quality stage, or it runs whole and is recorded, as in the set stage.
type target struct {
	model, dir string
	judged     bool
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

// set runs the set stage: each role of the set runs its tier 1 (TierOne) on the model the set
// assigns it, first alone, one role after another, then every role's at once, against the set's
// loaded models. A model run evaluates its model in its own roles beside the set's other role
// models; a set run, every role's assigned model. The units run whole, without their bars, and
// their scores are recorded, never judged: a tier's bar judges a model alone, in its quality
// stage, and the set stage holds the set to its memory and crossing. The stage gates two things, both over the
// concurrent phase: the pool's least free memory, read as capacity reads it, against the pool's
// floor; and no crossed answer, from rounds of nonce requests sent to every role's model at once
// while the tiers run (crossing). A unit that can't run fails the stage too, as a measurement
// that couldn't be taken. Each unit's output is kept in the run directory's set/alone and
// set/concurrent.
func (r *Runner) set(ctx context.Context, _ *Report) []Check {
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
	targetOf := func(a assignment, phase string) target {
		return target{model: a.model, dir: filepath.Join(r.Dir, "set", phase)}
	}

	alone := map[string][]Check{}
	aloneFree := map[string]Pool{}
	for _, a := range roles {
		r.logf("evaluate: set: %s tier 1 alone on %s\n", a.role, a.model)
		low, readings, _ := r.during(ctx, func() { alone[a.role] = r.tierOne(ctx, targetOf(a, phaseAlone), a.role) })
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
	c := &client{base: r.Router, hc: r.HTTP}
	together := map[string][]Check{}
	var mu sync.Mutex
	var cross crossing
	began := time.Now()
	low, readings, readErr := r.during(ctx, func() {
		done := make(chan struct{})
		var probe sync.WaitGroup
		probe.Go(func() { cross = r.cross(ctx, c, roles, done) })
		var tiers sync.WaitGroup
		for _, a := range roles {
			tiers.Go(func() {
				checks := r.tierOne(ctx, targetOf(a, phaseConcurrent), a.role)
				mu.Lock()
				together[a.role] = checks
				mu.Unlock()
			})
		}
		tiers.Wait()
		close(done)
		probe.Wait()
	})
	phase := time.Since(began).Seconds()

	var checks []Check
	for _, a := range roles {
		checks = append(checks, phased(alone[a.role], phaseAlone, nil)...)
		checks = append(checks, phased(together[a.role], phaseConcurrent, alone[a.role])...)
		ch := slowdown(a.role, alone[a.role], together[a.role])
		if p, ok := aloneFree[a.role]; ok {
			ch.Values["alone_pool_free_min_mib"] = float64(p.FreeMiB())
		}
		checks = append(checks, ch)
	}
	checks = append(checks, cross.check())
	if readings == 0 {
		return append(checks, broken("peak pool free", "", readErr))
	}
	peak := measure("peak pool free", "", float64(low.FreeMiB()), "MiB", AtLeast, r.Floors.Pool.FreeMiB)
	peak.Values = map[string]float64{"readings": float64(readings), "phase_wall_s": phase}
	peak.Detail = fmt.Sprintf("%d of %d MiB used at the least free of %d readings over the %.0fs every role's tier 1 ran at once",
		low.UsedMiB, low.TotalMiB, readings, phase)
	return append(checks, peak)
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
// units', recorded, never judged.
func slowdown(role string, alone, together []Check) Check {
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
	ch.Detail = fmt.Sprintf("%s: %.0fs at once against %.0fs alone; recorded, never judged", strings.Join(TierOne(role), " + "), t, a)
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
// A request that errored or came back empty carries nothing to cross, so it is recorded
// unanswered rather than crossed: an empty answer is a reliability fault the stage notes, not
// another request's content (on set A, Gemma 4 26B-A4B returned one empty answer of 22 under the
// full load). The check still needs each role checked at least once.
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
			cfg.Embed = append(cfg.Embed, concurrent.EmbedStream{Name: a.role, Embedder: embedder{c, a.model}, Inputs: capacityInputs})
			continue
		}
		cfg.Chat = append(cfg.Chat, concurrent.ChatStream{
			Name: a.role, Chatter: &recorder{c: c, model: a.model, role: a.role, media: r.Media}, Workers: 1,
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
