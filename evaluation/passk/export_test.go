package passk

import (
	"context"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"
)

// WithConform returns r running each cell through run in place of clutch's conform.Run.
func WithConform(r Runner, run func(context.Context, conform.Cell, conform.Options) conform.Report) Runner {
	r.runCell = run
	return r
}
