package passk

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
)

// Bar is the pass^k a consumer must reach, with no invalid request: OpenAI's bar for a gpt-oss
// implementation is over 90%, which the spike reads as at least 90%. Over nine tasks only
// nine of nine reach it: eight of nine is 88.9%.
const Bar = 0.9

// Trial is one run of a consumer's cell: what the suite found, and the consumer's model
// requests the router answered with an error.
type Trial struct {
	Report  conform.Report
	Invalid []wire.Exchange
}

// Row is a consumer's k trials, counted.
type Row struct {
	Consumer string
	// Version is the consumer's version, as the first trial that ran reported it.
	Version string
	// K is the number of trials.
	K int
	// Cells has one entry per capability, in conform.Capabilities' order.
	Cells []Cell
	// Skipped is why each skipped trial didn't run.
	Skipped []string
	// Invalid is the number of the consumer's model requests the router answered with an
	// error, over every trial.
	Invalid int
}

// Cell is one capability over a consumer's k trials. Its counts add up to K.
type Cell struct {
	Capability           string
	Pass, Fail, NA, Skip int
	// Reasons are the reasons of the trials that didn't pass, in trial order.
	Reasons []string
}

// Passed reports whether every one of the k trials passed.
func (c Cell) Passed(k int) bool { return k > 0 && c.Pass == k }

// Tally counts a consumer's trials into its Row. A skipped trial counts as a skip in every
// capability.
func Tally(consumer string, trials []Trial) Row {
	r := Row{Consumer: consumer, K: len(trials)}
	r.Cells = make([]Cell, len(conform.Capabilities))
	for i, c := range conform.Capabilities {
		r.Cells[i].Capability = c.Name
	}
	for _, t := range trials {
		r.Invalid += len(t.Invalid)
		if r.Version == "" && t.Report.Skipped == "" {
			r.Version = t.Report.Version
		}
		if t.Report.Skipped != "" {
			r.Skipped = append(r.Skipped, t.Report.Skipped)
		}
		for i := range r.Cells {
			c := &r.Cells[i]
			status, reason := conform.Skip, t.Report.Skipped
			if i < len(t.Report.Results) {
				status, reason = t.Report.Results[i].Status, t.Report.Results[i].Reason
			}
			switch status {
			case conform.Pass:
				c.Pass++
				continue
			case conform.Fail:
				c.Fail++
			case conform.NA:
				c.NA++
			default:
				c.Skip++
			}
			c.Reasons = append(c.Reasons, reason)
		}
	}
	return r
}

// Passed is the number of tasks, capabilities, that passed at k.
func (r Row) Passed() int {
	n := 0
	for _, c := range r.Cells {
		if c.Passed(r.K) {
			n++
		}
	}
	return n
}

// PassK is pass^k: the share of the tasks that passed at k.
func (r Row) PassK() float64 {
	if len(r.Cells) == 0 {
		return 0
	}
	return float64(r.Passed()) / float64(len(r.Cells))
}

// MeetsBar reports whether the consumer meets the bar: no invalid request, and pass^k at
// least Bar.
func (r Row) MeetsBar() bool { return r.Invalid == 0 && r.PassK() >= Bar }

// chatRoutes are the routes a consumer's model requests take: OpenAI chat and responses, and
// Anthropic messages. Anthropic's count_tokens route, beneath messages, generates nothing.
var chatRoutes = []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"}

// Invalid returns the exchanges in xs that are model requests the router answered with an
// error: an error status, or a stream that began with 200 and then carried an error event, as
// llama.cpp sends a parse failure ("does not match the expected peg-native format") found
// after the first tokens have streamed. A request the consumer cancelled before the router
// answered, which the Recorder records as a 502 carrying the cancellation, is the cancel
// capability's, not an invalid request.
func Invalid(xs []wire.Exchange) []wire.Exchange {
	var out []wire.Exchange
	for _, x := range xs {
		if !isChat(x.Path) {
			continue
		}
		switch {
		case x.Status == http.StatusBadGateway && strings.Contains(x.Response, "context canceled"):
		case x.Status >= http.StatusBadRequest, streamedError(x.Response):
			out = append(out, x)
		}
	}
	return out
}

// streamedError reports whether body, a server-sent event stream, carries an error event: an
// OpenAI chunk whose data is an error object, or an Anthropic error event.
func streamedError(body string) bool {
	for _, ev := range wire.Events(body) {
		var data struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
		}
		if ev.Name == "error" {
			return true
		}
		if json.Unmarshal([]byte(ev.Data), &data) == nil && (data.Type == "error" || len(data.Error) > 0 && string(data.Error) != "null") {
			return true
		}
	}
	return false
}

func isChat(path string) bool {
	path, _, _ = strings.Cut(path, "?")
	return slices.Contains(chatRoutes, path)
}
