package wire_test

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
)

// secret is a bearer token the tests send, which must never reach a record or the router.
const secret = "sk-ant-oat01-do-not-record"

// serve starts a Recorder in front of upstream and returns it with its URL.
func serve(t *testing.T, upstream string) (*wire.Recorder, string) {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	rec := wire.New(target)
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

// TestRecorder shows the Recorder forwards a request under its own path, returns the router's
// response unchanged, and records both bodies, with every credential's value redacted in the
// record and dropped from the forwarded request.
func TestRecorder(t *testing.T) {
	var forwarded http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Clone()
		if r.URL.RequestURI() != "/v1/messages?beta=true" {
			http.Error(w, "wrong path "+r.URL.RequestURI(), http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append([]byte(`{"echo":`), append(body, '}')...))
	}))
	defer up.Close()
	rec, base := serve(t, up.URL)

	req, _ := http.NewRequest(http.MethodPost, base+"/v1/messages?beta=true", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Api-Key", secret)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(got) != `{"echo":{"model":"m"}}` {
		t.Fatalf("response %s %q, want the router's", resp.Status, got)
	}
	for _, h := range []string{"Authorization", "X-Api-Key"} {
		if v := forwarded.Get(h); v != "" {
			t.Errorf("the router got %s %q, want none", h, v)
		}
	}
	if forwarded.Get("Anthropic-Version") != "2023-06-01" {
		t.Errorf("the router got Anthropic-Version %q, want the consumer's", forwarded.Get("Anthropic-Version"))
	}

	xs := rec.Exchanges()
	if len(xs) != 1 {
		t.Fatalf("%d exchanges, want 1", len(xs))
	}
	x := xs[0]
	if x.Method != http.MethodPost || x.Path != "/v1/messages?beta=true" || x.Status != http.StatusOK ||
		string(x.Request) != `{"model":"m"}` || x.Response != string(got) {
		t.Errorf("exchange %+v doesn't match the request and response", x)
	}
	if x.Header.Get("Authorization") != wire.Redacted || x.Header.Get("X-Api-Key") != wire.Redacted {
		t.Errorf("recorded Authorization %q, X-Api-Key %q; want both %q",
			x.Header.Get("Authorization"), x.Header.Get("X-Api-Key"), wire.Redacted)
	}
	var out bytes.Buffer
	if err := rec.WriteJSONL(&out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) {
		t.Errorf("the record carries the token:\n%s", out.String())
	}
}

// TestRecorderStreams shows a streamed response reaches the consumer event by event, before
// the router has finished it, and records whole.
func TestRecorderStreams(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: one\ndata: {\"n\":1}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-time.After(10 * time.Second):
		}
		_, _ = io.WriteString(w, "event: two\ndata: {\"n\":2}\n\n")
	}))
	defer up.Close()
	rec, base := serve(t, up.URL)

	resp, err := http.Post(base+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	first, err := bufio.NewReader(resp.Body).ReadString('\n')
	close(release)
	if err != nil || first != "event: one\n" {
		t.Fatalf("first line %q, %v; want the first event before the router finished", first, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// The exchange is recorded once the proxy closes the router's body, just after the
	// consumer's copy ends.
	var xs []wire.Exchange
	for deadline := time.Now().Add(5 * time.Second); len(xs) == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		xs = rec.Exchanges()
	}
	if len(xs) != 1 {
		t.Fatalf("%d exchanges, want 1", len(xs))
	}
	want := []wire.Event{{Name: "one", Data: `{"n":1}`}, {Name: "two", Data: `{"n":2}`}}
	if got := wire.Events(xs[0].Response); !reflect.DeepEqual(got, want) {
		t.Errorf("recorded events %v, want %v", got, want)
	}
}

// TestRecorderUnreachable shows a request the router never answered is recorded as a 502.
func TestRecorderUnreachable(t *testing.T) {
	up := httptest.NewServer(http.NotFoundHandler())
	up.Close()
	rec, base := serve(t, up.URL)
	resp, err := http.Post(base+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	xs := rec.Exchanges()
	if resp.StatusCode != http.StatusBadGateway || len(xs) != 1 || xs[0].Status != http.StatusBadGateway {
		t.Errorf("status %s, exchanges %+v; want one 502", resp.Status, xs)
	}
}

func TestEvents(t *testing.T) {
	cases := map[string]struct {
		body string
		want []wire.Event
	}{
		"named":      {"event: a\ndata: 1\n\nevent: b\ndata: 2\n\n", []wire.Event{{"a", "1"}, {"b", "2"}}},
		"data only":  {"data: {\"x\":1}\n\ndata: [DONE]\n\n", []wire.Event{{"", `{"x":1}`}, {"", "[DONE]"}}},
		"multi-line": {"event: a\ndata: 1\ndata: 2\n\n", []wire.Event{{"a", "1\n2"}}},
		"unended":    {"event: a\ndata:1", []wire.Event{{"a", "1"}}},
		"not stream": {`{"x":1}`, nil},
	}
	for name, tc := range cases {
		if got := wire.Events(tc.body); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Events = %v, want %v", name, got, tc.want)
		}
	}
}
