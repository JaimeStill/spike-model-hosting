//go:build !unix

package gate

import (
	"context"
	"os/exec"
)

// groupCommand is exec.CommandContext where there are no process groups: cancelling ctx kills
// the command alone.
func groupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
