package gate

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/swap"
)

// Warm is how a run started from freshly loaded models: the set reloaded over the router's API,
// the idle pool read after it settled, each of the set's models driven to its high-water mark,
// and the pool read again at that warmed peak, the reading every pool floor is judged at. A model
// keeps the largest working memory it has needed until it is reloaded, so without the reload one
// run's growth would carry into the next (docs/findings/memory-budget.md, "The pool over a day of
// runs"), and without the warm-up the floor would be judged before the run's own requests grew it.
type Warm struct {
	// Reload is each of the set's models' unload and load, in the order they ran, and ReloadS the
	// whole reload's wall time; SettleS is the wait after it before the idle reading.
	Reload  []WarmStep `json:"reload"`
	ReloadS float64    `json:"reload_s"`
	SettleS float64    `json:"settle_s"`
	// Idle is the pool read after the reload settled, and Peak after the warm-up; each is nil
	// when its reading failed, IdleErr or PeakErr saying why.
	Idle    *Pool  `json:"idle,omitempty"`
	IdleErr string `json:"idle_err,omitempty"`
	// Requests are the warm-up's requests, model by model, in the set's order.
	Requests []WarmRequest `json:"requests"`
	Peak     *Pool         `json:"peak,omitempty"`
	PeakErr  string        `json:"peak_err,omitempty"`
	// Reused marks a resumed run's copy of the warm-up of the run it resumed, which reloaded
	// nothing.
	Reused bool `json:"reused,omitempty"`
}

// WarmStep is one model's unload or load in the reload.
type WarmStep struct {
	Model string  `json:"model"`
	Op    string  `json:"op"`
	TookS float64 `json:"took_s"`
}

// WarmRequest is one request of the warm-up: what it sent (Name), to which model and for which
// role, with the media it carried (Detail), its wall time, and why it failed (Err) when it did.
type WarmRequest struct {
	Model  string  `json:"model"`
	Role   string  `json:"role,omitempty"`
	Name   string  `json:"name"`
	Detail string  `json:"detail,omitempty"`
	WallS  float64 `json:"wall_s"`
	Err    string  `json:"err,omitempty"`
}

// The warm-up's requests, by Name.
const (
	// WarmPrompt is an 8192-token prompt, every generative model's: prefilled in full ubatches,
	// it grows the language model's backend buffers as the deep stage's 8192-token prompts do. A
	// model whose context is under it and warmReserve, as a sweep step's can be, takes its c less
	// warmReserve.
	WarmPrompt = "8192-token prompt"
	// WarmImage is the vision role's largest image in the committed tier lists, WarmClip the
	// audio role's longest clip in them, and WarmInput the embedding role's largest input, one
	// that fills its physical batch.
	WarmImage = "largest image"
	WarmClip  = "longest clip"
	WarmInput = "largest input"
)

// embedReserve is the room the embedding model's largest input leaves in its physical batch
// (c = b = ub) for the special tokens the server adds around an input.
const embedReserve = 8

// warmReserve is the room the warm-up's prompt leaves in a model's context, when the context is
// under the prompt and a reserve, for the chat template's tokens and the 16-token answer.
const warmReserve = 256

// defaultSettle is the wait after a reload before the idle reading, as the manual procedure
// waited (docs/findings/memory-budget.md, "The pool over a day of runs").
const defaultSettle = 30 * time.Second

// Peak is a tier list's largest image or longest clip, as Peaks finds it.
type Peak struct {
	// Tier names the tier list it came from, such as vision-vstar, and ID its sample there.
	Tier string `json:"tier"`
	ID   string `json:"id"`
	// File is the media's path, in the directory it was left in.
	File string `json:"file"`
	// Width and Height are an image's pixels; AudioS is a clip's length.
	Width  int     `json:"width,omitempty"`
	Height int     `json:"height,omitempty"`
	AudioS float64 `json:"audio_s,omitempty"`
}

// PeakFile is the file Peaks leaves its find in, in the directory it is given.
const PeakFile = "peak.json"

// Peaks finds the largest image (role RoleVision) or the longest clip (RoleAudio) every tier
// list of the role's under tiers sends, and leaves it in out, beside PeakFile, which describes it
// (ReadPeak).
type Peaks interface {
	Peak(ctx context.Context, role, tiers, out string) error
}

// Peak finds the role's peak through vision_quality.py peak or asr_quality.py peak, and fails
// when uv does.
func (u UV) Peak(ctx context.Context, role, tiers, out string) error {
	script := map[string]string{RoleVision: "vision_quality.py", RoleAudio: "asr_quality.py"}[role]
	if script == "" {
		return fmt.Errorf("the %s role has no peak media", role)
	}
	if err := u.run(ctx, script, "peak", "--tiers", tiers, "--output", out); err != nil {
		return fmt.Errorf("uv run %s peak: %w", script, err)
	}
	return nil
}

// ReadPeak reads the Peak left in dir, its File made absolute within dir.
func ReadPeak(dir string) (Peak, error) {
	b, err := os.ReadFile(filepath.Join(dir, PeakFile))
	if err != nil {
		return Peak{}, err
	}
	var p Peak
	if err := json.Unmarshal(b, &p); err != nil {
		return Peak{}, fmt.Errorf("%s: %w", filepath.Join(dir, PeakFile), err)
	}
	if p.File == "" {
		return Peak{}, fmt.Errorf("%s names no file", filepath.Join(dir, PeakFile))
	}
	if !filepath.IsAbs(p.File) {
		p.File = filepath.Join(dir, p.File)
	}
	return p, nil
}

// warm reloads the set, reads the idle pool once it settles, drives each of the set's models to
// its high-water mark, and reads the pool at that warmed peak. A reload that fails is the run's
// error: the set may be left unloaded. A warm-up request or a reading that fails is recorded, and
// fails the floor judged on it (warmedCheck).
func (r *Runner) warm(ctx context.Context) (*Warm, error) {
	w := &Warm{}
	// The peak media are found first, on the laptop: the scan reads the tier lists' datasets,
	// which takes a while, and the settle and the readings should follow the reload directly.
	peaks := map[string]Peak{}
	peakErrs := map[string]error{}
	for _, a := range r.assignments() {
		if a.role != RoleVision && a.role != RoleAudio {
			continue
		}
		peaks[a.role], peakErrs[a.role] = r.findPeak(ctx, a.role)
	}

	r.logf("evaluate: reload set %s\n", r.Set.Name)
	began := time.Now()
	rep, err := swap.Reload(ctx, swap.Config{Router: r.Router, HTTP: r.HTTP, Models: r.Set.Models, Interval: r.PollEvery})
	w.ReloadS = time.Since(began).Seconds()
	for _, s := range rep.Unloads {
		w.Reload = append(w.Reload, WarmStep{Model: s.Model, Op: "unload", TookS: s.Took.Seconds()})
	}
	for _, s := range rep.Loads {
		w.Reload = append(w.Reload, WarmStep{Model: s.Model, Op: "load", TookS: s.Took.Seconds()})
	}
	if err != nil {
		return w, fmt.Errorf("reloading set %s: %w", r.Set.Name, err)
	}

	settle := cmp.Or(r.Settle, defaultSettle)
	select {
	case <-ctx.Done():
		return w, ctx.Err()
	case <-time.After(settle):
	}
	w.SettleS = settle.Seconds()
	w.Idle, w.IdleErr = r.read(ctx)

	r.logf("evaluate: warm set %s to its peak\n", r.Set.Name)
	c := &client{base: r.Router, hc: r.HTTP}
	for _, m := range r.Set.Models {
		w.Requests = append(w.Requests, r.warmModel(ctx, c, m, peaks, peakErrs)...)
	}
	w.Peak, w.PeakErr = r.read(ctx)
	return w, nil
}

// read reads the pool once, returning the reading or why it failed.
func (r *Runner) read(ctx context.Context) (*Pool, string) {
	p, err := r.Footprint.Read(ctx)
	if err != nil {
		return nil, err.Error()
	}
	return &p, ""
}

// findPeak finds the role's peak media into the run directory's warm/<role>, or returns the one
// the runner's peaks keep.
func (r *Runner) findPeak(ctx context.Context, role string) (Peak, error) {
	if p, ok := r.peaks[role]; ok {
		return p, nil
	}
	if r.Peaks == nil {
		return Peak{}, errors.New("the runner has no peak finder")
	}
	if r.Dir == "" {
		return Peak{}, errors.New("the runner has no run directory")
	}
	out := filepath.Join(r.Dir, "warm", role)
	if err := os.MkdirAll(out, 0o750); err != nil {
		return Peak{}, err
	}
	if err := r.Peaks.Peak(ctx, role, r.Tiers, out); err != nil {
		return Peak{}, err
	}
	p, err := ReadPeak(out)
	if err == nil && r.peaks != nil {
		r.peaks[role] = p
	}
	return p, err
}

// warmModel sends model its warm-up: the 8192-token prompt for a generative model, then each role
// the set assigns it its high-water request.
func (r *Runner) warmModel(ctx context.Context, c *client, model string, peaks map[string]Peak, peakErrs map[string]error) []WarmRequest {
	var out []WarmRequest
	send := func(role, name string, fn func(context.Context) (string, error)) {
		rctx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		began := time.Now()
		detail, err := fn(rctx)
		req := WarmRequest{Model: model, Role: role, Name: name, Detail: detail, WallS: time.Since(began).Seconds()}
		if err != nil {
			req.Err = err.Error()
		}
		out = append(out, req)
	}
	m := r.Profile.Model(model)
	if m != nil && m.Kind == profile.KindGenerative {
		// A sweep step can give the model less context than the prompt: it is cut to fit, room
		// left for the template and the answer, the most the model can be driven to.
		n := min(longPromptTokens, m.C-warmReserve)
		send("", WarmPrompt, func(ctx context.Context) (string, error) {
			text, err := c.fitText(ctx, model, n)
			if err != nil {
				return "", err
			}
			comp, err := c.chatUncached(ctx, model, text+"\n\nIn one sentence, what is the passage above about?", 16)
			return fmt.Sprintf("%d prompt tokens", comp.Timings.PromptN), err
		})
	}
	for _, a := range r.assignments() {
		if a.model != model {
			continue
		}
		switch a.role {
		case RoleVision:
			send(a.role, WarmImage, func(ctx context.Context) (string, error) {
				p, img, err := peakMedia(peaks[a.role], peakErrs[a.role])
				if err != nil {
					return "", err
				}
				detail := fmt.Sprintf("%s %s, %dx%d", p.Tier, p.ID, p.Width, p.Height)
				_, err = c.chatUncached(ctx, model, []map[string]any{imageData(img), textPart("Describe this image in one sentence.")}, 16)
				return detail, err
			})
		case RoleAudio:
			send(a.role, WarmClip, func(ctx context.Context) (string, error) {
				p, wav, err := peakMedia(peaks[a.role], peakErrs[a.role])
				if err != nil {
					return "", err
				}
				detail := fmt.Sprintf("%s %s, %.1fs", p.Tier, p.ID, p.AudioS)
				_, err = c.complete(ctx, transcriptionBody(model, r.asrPrompt(model), wav, ""))
				return detail, err
			})
		case RoleEmbedding:
			send(a.role, WarmInput, func(ctx context.Context) (string, error) {
				if m == nil || m.C <= embedReserve {
					return "", fmt.Errorf("the profile gives %s no context to fill", model)
				}
				n := m.C - embedReserve
				text, err := c.fitText(ctx, model, n)
				if err != nil {
					return "", err
				}
				_, err = c.embed(ctx, model, []string{text})
				return fmt.Sprintf("%d tokens", n), err
			})
		}
	}
	return out
}

// peakMedia reads a found peak's file, or returns why it wasn't found.
func peakMedia(p Peak, findErr error) (Peak, []byte, error) {
	if findErr != nil {
		return p, nil, fmt.Errorf("finding it: %w", findErr)
	}
	b, err := os.ReadFile(p.File)
	return p, b, err
}

// imageData is an image's content part, its media type sniffed from its bytes.
func imageData(b []byte) map[string]any {
	part := imagePart(b)
	url := part["image_url"].(map[string]any)["url"].(string)
	part["image_url"].(map[string]any)["url"] = strings.Replace(url, "image/png", http.DetectContentType(b), 1)
	return part
}

// warmedCheck judges the pool's free memory at the warmed peak against the floor, the idle
// reading and each reading's system RAM beside it. A warm-up request that failed, or a reading
// that couldn't be taken, makes it a measurement that couldn't be taken: the peak wasn't reached.
func (r *Runner) warmedCheck(w *Warm) Check {
	const name = "warmed pool free"
	if w == nil {
		return broken(name, "", errors.New("the run recorded no reload and warm-up"))
	}
	var failed []string
	for _, q := range w.Requests {
		if q.Err != "" {
			failed = append(failed, fmt.Sprintf("%s's %s: %s", q.Model, q.Name, q.Err))
		}
	}
	switch {
	case len(failed) > 0:
		return broken(name, "", fmt.Errorf("the warm-up didn't reach the peak: %s", strings.Join(failed, "; ")))
	case w.Peak == nil:
		return broken(name, "", errors.New(w.PeakErr))
	}
	p := *w.Peak
	ch := measure(name, "", float64(p.FreeMiB()), "MiB", AtLeast, r.Floors.Pool.FreeMiB)
	ch.Values = poolValues(p)
	ch.Detail = fmt.Sprintf("%d of %d MiB used after the warm-up%s", p.UsedMiB, p.TotalMiB, ramDetail(p))
	if w.Idle != nil {
		ch.Values["idle_pool_free_mib"] = float64(w.Idle.FreeMiB())
		ch.Values["warm_growth_mib"] = float64(p.UsedMiB - w.Idle.UsedMiB)
		ch.Detail += fmt.Sprintf("; idle after the reload, %d MiB free", w.Idle.FreeMiB())
	}
	return ch
}
