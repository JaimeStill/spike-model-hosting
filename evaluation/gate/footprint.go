package gate

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// Pool is one reading of the memory pool a set's models share.
type Pool struct {
	UsedMiB, TotalMiB int
}

// FreeMiB is the pool's free memory.
func (p Pool) FreeMiB() int { return p.TotalMiB - p.UsedMiB }

// Footprint reads the pool on the host that serves the router. It must be safe for concurrent
// use.
type Footprint interface {
	Read(ctx context.Context) (Pool, error)
}

// SSH reads the pool over ssh with amdgpu_top, as evaluation/scripts/footprint.sh does: the GTT
// figure of its header line, the pool Strix Halo's unified memory loads models into. It is
// read-only on the host: one non-interactive ssh a reading, amdgpu_top run with --no-pc so it
// reads no performance counter.
type SSH struct {
	// Dest is the ssh destination, such as framework.
	Dest string
}

// Read runs amdgpu_top on Dest and parses its pool.
func (s SSH) Read(ctx context.Context) (Pool, error) {
	out, err := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", s.Dest, "amdgpu_top -p --no-pc").Output()
	if err != nil {
		return Pool{}, fmt.Errorf("ssh %s amdgpu_top: %w", s.Dest, err)
	}
	return ParseAmdgpuTop(out)
}

// gttHeader matches amdgpu_top -p's device line, such as
// "0000:c3:00.0 (Radeon 8060S Graphics), VRAM   496/  512 MiB, GTT 94971/98304 MiB".
var gttHeader = regexp.MustCompile(`GTT\s+(\d+)\s*/\s*(\d+)\s+MiB`)

// ParseAmdgpuTop returns the pool from amdgpu_top -p's output: the first GTT used/total figure,
// the device's, ahead of any process's.
func ParseAmdgpuTop(out []byte) (Pool, error) {
	m := gttHeader.FindSubmatch(out)
	if m == nil {
		return Pool{}, fmt.Errorf("amdgpu_top's output holds no GTT used/total line")
	}
	used, _ := strconv.Atoi(string(m[1]))
	total, _ := strconv.Atoi(string(m[2]))
	return Pool{UsedMiB: used, TotalMiB: total}, nil
}
