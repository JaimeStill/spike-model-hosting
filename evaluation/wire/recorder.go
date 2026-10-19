package wire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Redacted stands in a record for the value of a credential header.
const Redacted = "[redacted]"

// credentials are the headers that carry a secret: the Recorder records that each was sent,
// never its value, and doesn't forward it.
var credentials = []string{"Authorization", "X-Api-Key", "Proxy-Authorization", "Cookie"}

// Exchange is one request through the Recorder and its response.
type Exchange struct {
	// Time is when the request arrived.
	Time   time.Time `json:"time"`
	Method string    `json:"method"`
	// Path is the request's path and query, as the consumer sent it.
	Path string `json:"path"`
	// Header is the request's headers, each credential's value Redacted.
	Header  http.Header     `json:"header"`
	Request json.RawMessage `json:"request,omitempty"`
	// RequestText holds a request body that isn't JSON.
	RequestText string `json:"request_text,omitempty"`
	// Status is the response's status code; 502 when the router couldn't be reached.
	Status int `json:"status"`
	// Response is the response body as it arrived: JSON, or a server-sent event stream.
	Response string `json:"response"`
}

// Recorder is a reverse proxy to one target that records every exchange. It is safe for
// concurrent use.
type Recorder struct {
	proxy *httputil.ReverseProxy

	mu        sync.Mutex
	exchanges []Exchange
}

// New returns a Recorder that forwards each request to target, the router's root, under the
// request's own path.
func New(target *url.URL) *Recorder {
	r := &Recorder{}
	r.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			for _, h := range credentials {
				pr.Out.Header.Del(h)
			}
			// An uncompressed response records as text.
			pr.Out.Header.Del("Accept-Encoding")
		},
		// A negative interval flushes each write as it arrives, so a stream reaches the
		// consumer token by token, as it would without the Recorder.
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			x := resp.Request.Context().Value(exchangeKey{}).(*Exchange)
			x.Status = resp.StatusCode
			resp.Body = &recordingBody{ReadCloser: resp.Body, done: func(body []byte) {
				x.Response = string(body)
				r.add(*x)
			}}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			x := req.Context().Value(exchangeKey{}).(*Exchange)
			x.Status, x.Response = http.StatusBadGateway, err.Error()
			r.add(*x)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return r
}

type exchangeKey struct{}

// ServeHTTP forwards req to the target and records the exchange once its response has been
// read to the end.
func (r *Recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	x := &Exchange{Time: time.Now(), Method: req.Method, Path: req.URL.RequestURI(), Header: redact(req.Header)}
	if json.Valid(body) {
		x.Request = body
	} else {
		x.RequestText = string(body)
	}
	r.proxy.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), exchangeKey{}, x)))
}

// Exchanges returns a copy of the exchanges recorded so far, in the order their responses
// ended. An exchange is recorded as the proxy closes the router's body, which can follow the
// consumer's reading its response, so a recording is complete only once every handler has
// returned: read it after the server serving the Recorder has shut down (http.Server.Shutdown,
// or httptest.Server.Close), which waits for them.
func (r *Recorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Exchange(nil), r.exchanges...)
}

// WriteJSONL writes the exchanges recorded so far to w, one JSON object a line.
func (r *Recorder) WriteJSONL(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, x := range r.Exchanges() {
		if err := enc.Encode(x); err != nil {
			return err
		}
	}
	return nil
}

func (r *Recorder) add(x Exchange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exchanges = append(r.exchanges, x)
}

// redact returns a copy of h with each credential's value Redacted.
func redact(h http.Header) http.Header {
	out := h.Clone()
	for _, name := range credentials {
		if vs := out.Values(name); len(vs) > 0 {
			out[http.CanonicalHeaderKey(name)] = []string{Redacted}
		}
	}
	return out
}

// recordingBody keeps a copy of what the proxy reads of a response, and hands it to done once,
// when the proxy closes it.
type recordingBody struct {
	io.ReadCloser
	buf  bytes.Buffer
	once sync.Once
	done func([]byte)
}

func (b *recordingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.buf.Write(p[:n])
	return n, err
}

func (b *recordingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { b.done(b.buf.Bytes()) })
	return err
}

// Event is one server-sent event: its event name, empty when it has none, and its data.
type Event struct {
	Name string
	Data string
}

// Events splits a server-sent event stream into its events, joining an event's data lines
// with newlines. A body that isn't a stream has no events.
func Events(body string) []Event {
	var (
		out  []Event
		cur  Event
		data []string
	)
	flush := func() {
		if cur.Name != "" || len(data) > 0 {
			cur.Data = strings.Join(data, "\n")
			out = append(out, cur)
		}
		cur, data = Event{}, nil
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event:"):
			cur.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
	return out
}
