package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// requestTimeout bounds one request to the router: each answers from its state, without
// generating. A load or unload request returns once the router has taken it, not once the model
// is loaded or unloaded.
const requestTimeout = 30 * time.Second

// The status values GET /models gives a model that this package reads. The router also lists a
// model as loading, downloading (fetching its files from Hugging Face) or sleeping.
const (
	StatusLoaded   = "loaded"
	StatusUnloaded = "unloaded"
)

// Model is one model GET /models lists, with its status.
type Model struct {
	// ID is the model's router ID.
	ID string
	// Status is the model's status value, such as StatusLoaded or StatusUnloaded.
	Status string
	// Failed is whether the model's last load failed: the router lists it unloaded, with the
	// exit code of the instance that failed.
	Failed bool
	// ExitCode is that failed instance's exit code, when Failed is set.
	ExitCode int
}

// Models returns each model GET /models on the router at base lists, in the order it lists them.
func Models(ctx context.Context, hc *http.Client, base string) ([]Model, error) {
	var got struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Value    string `json:"value"`
				Failed   bool   `json:"failed"`
				ExitCode int    `json:"exit_code"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := do(ctx, hc, http.MethodGet, base+"/models", nil, &got); err != nil {
		return nil, err
	}
	models := make([]Model, len(got.Data))
	for i, m := range got.Data {
		models[i] = Model{ID: m.ID, Status: m.Status.Value, Failed: m.Status.Failed, ExitCode: m.Status.ExitCode}
	}
	return models, nil
}

// Statuses returns the status of each model GET /models on the router at base lists, by router
// ID, such as "loaded" or "unloaded".
func Statuses(ctx context.Context, hc *http.Client, base string) (map[string]string, error) {
	models, err := Models(ctx, hc, base)
	if err != nil {
		return nil, err
	}
	status := make(map[string]string, len(models))
	for _, m := range models {
		status[m.ID] = m.Status
	}
	return status, nil
}

// CheckLoaded checks GET /models on the router at base lists each of ids as loaded, and fails
// naming the first that isn't, with its status.
func CheckLoaded(ctx context.Context, hc *http.Client, base string, ids ...string) error {
	status, err := Statuses(ctx, hc, base)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if s := status[id]; s != StatusLoaded {
			return fmt.Errorf("the router lists %s as %q, not loaded; the run never loads a model", id, s)
		}
	}
	return nil
}

// Build returns the build of the router at base, GET /props's build_info.
func Build(ctx context.Context, hc *http.Client, base string) (string, error) {
	var got struct {
		BuildInfo string `json:"build_info"`
	}
	err := do(ctx, hc, http.MethodGet, base+"/props", nil, &got)
	return got.BuildInfo, err
}

// ReloadPresets asks the router at base to read its presets file again, GET /models?reload=1,
// with no restart: the router unloads each running model whose section changed and loads none, so
// a model it unloaded is loaded again over Load.
func ReloadPresets(ctx context.Context, hc *http.Client, base string) error {
	var got json.RawMessage
	return do(ctx, hc, http.MethodGet, base+"/models?reload=1", nil, &got)
}

// Served is what a loaded model's server reports it runs at, GET /props?model=<id>: its slots,
// total_slots, and its context, default_generation_settings.n_ctx, the whole of it a unified KV
// pool's slots share.
type Served struct {
	Slots int
	NCtx  int
}

// ServedAt returns the slots and context the model id's server runs at, on the router at base;
// the request carries autoload=false, so it never loads the model.
func ServedAt(ctx context.Context, hc *http.Client, base, id string) (Served, error) {
	var got struct {
		TotalSlots int `json:"total_slots"`
		Settings   struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	u := base + "/props?autoload=false&model=" + url.QueryEscape(id)
	if err := do(ctx, hc, http.MethodGet, u, nil, &got); err != nil {
		return Served{}, err
	}
	return Served{Slots: got.TotalSlots, NCtx: got.Settings.NCtx}, nil
}

// Load asks the router at base to load the model id, POST /models/load. It returns once the
// router has taken the request; the model loads in the background, downloading its files first
// when the router hasn't them, and GET /models follows it.
func Load(ctx context.Context, hc *http.Client, base, id string) error {
	return command(ctx, hc, base+"/models/load", id)
}

// Unload asks the router at base to unload the model id, POST /models/unload. It returns once
// the router has taken the request; GET /models follows the unload.
func Unload(ctx context.Context, hc *http.Client, base, id string) error {
	return command(ctx, hc, base+"/models/unload", id)
}

// command sends {"model": id} to u and fails unless the router answers {"success": true}.
func command(ctx context.Context, hc *http.Client, u, id string) error {
	var got struct {
		Success bool `json:"success"`
	}
	if err := do(ctx, hc, http.MethodPost, u, map[string]string{"model": id}, &got); err != nil {
		return err
	}
	if !got.Success {
		return fmt.Errorf("POST %s %s: the router didn't answer success", u, id)
	}
	return nil
}

// do sends a request to u, in as its JSON body when it isn't nil, and decodes the JSON answer
// into out. An answer other than 200 fails, carrying the start of the router's answer.
func do(ctx context.Context, hc *http.Client, method, u string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %s: %s", method, u, resp.Status, bytes.TrimSpace(msg))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: %w", method, u, err)
	}
	return nil
}
