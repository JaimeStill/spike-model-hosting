package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// requestTimeout bounds one read of the router: each answers from its state, without generating.
const requestTimeout = 30 * time.Second

// Statuses returns the status of each model GET /models on the router at base lists, by router
// ID, such as "loaded" or "unloaded".
func Statuses(ctx context.Context, hc *http.Client, base string) (map[string]string, error) {
	var got struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := getJSON(ctx, hc, base+"/models", &got); err != nil {
		return nil, err
	}
	status := make(map[string]string, len(got.Data))
	for _, m := range got.Data {
		status[m.ID] = m.Status.Value
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
		if s := status[id]; s != "loaded" {
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
	err := getJSON(ctx, hc, base+"/props", &got)
	return got.BuildInfo, err
}

func getJSON(ctx context.Context, hc *http.Client, u string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	return nil
}
