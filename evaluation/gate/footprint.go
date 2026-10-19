package gate

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Pool is one reading of the memory pool a set's models share, with the host's system RAM beside
// it.
type Pool struct {
	UsedMiB  int `json:"used_mib"`
	TotalMiB int `json:"total_mib"`
	// RAM is the host's system memory at the same reading; nil for a reading that took none.
	RAM *RAM `json:"ram,omitempty"`
}

// FreeMiB is the pool's free memory.
func (p Pool) FreeMiB() int { return p.TotalMiB - p.UsedMiB }

// RAM is the host's system memory at one reading: used and available, as free reports them, and
// each model process's resident memory. The prompt cache (--cache-ram) and the image embeddings
// live here, not in the pool.
type RAM struct {
	UsedMiB      int `json:"used_mib"`
	AvailableMiB int `json:"available_mib"`
	TotalMiB     int `json:"total_mib"`
	// RSSMiB is each model process's resident memory, by the router ID its --alias names.
	RSSMiB map[string]int `json:"rss_mib,omitempty"`
}

// Footprint reads the pool on the host that serves the router. It must be safe for concurrent
// use.
type Footprint interface {
	Read(ctx context.Context) (Pool, error)
}

// SSH reads the pool over ssh with amdgpu_top, as evaluation/scripts/footprint.sh does: the GTT
// figure of its header line, the pool Strix Halo's unified memory loads models into; and system
// RAM beside it, from free -b and ps. It is read-only on the host: one non-interactive ssh a
// reading, amdgpu_top run with --no-pc so it reads no performance counter.
type SSH struct {
	// Dest is the ssh destination, such as framework.
	Dest string
}

// sshReading is the one command a reading runs: amdgpu_top, then free and ps, each section after a
// marker line ParseFootprint splits on.
const sshReading = "amdgpu_top -p --no-pc && echo " + freeMarker + " && free -b && echo " + psMarker + " && ps -eo rss=,args="

const (
	freeMarker = "@@free"
	psMarker   = "@@ps"
)

// Read runs the reading on Dest and parses its pool and RAM.
func (s SSH) Read(ctx context.Context) (Pool, error) {
	out, err := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", s.Dest, sshReading).Output()
	if err != nil {
		return Pool{}, fmt.Errorf("ssh %s amdgpu_top, free, ps: %w", s.Dest, err)
	}
	return ParseFootprint(out)
}

// ParseFootprint parses one SSH reading: amdgpu_top -p's pool (ParseAmdgpuTop), then, after
// their markers, free -b's memory line and ps -eo rss=,args='s llama-server processes
// (ParseRAM).
func ParseFootprint(out []byte) (Pool, error) {
	gpu, rest, ok := bytes.Cut(out, []byte(freeMarker+"\n"))
	if !ok {
		return Pool{}, fmt.Errorf("the reading holds no %s section", freeMarker)
	}
	free, ps, ok := bytes.Cut(rest, []byte(psMarker+"\n"))
	if !ok {
		return Pool{}, fmt.Errorf("the reading holds no %s section", psMarker)
	}
	p, err := ParseAmdgpuTop(gpu)
	if err != nil {
		return Pool{}, err
	}
	ram, err := ParseRAM(free, ps)
	if err != nil {
		return Pool{}, err
	}
	p.RAM = &ram
	return p, nil
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

// aliasArg matches a llama-server instance's --alias, the router ID the router starts it under.
var aliasArg = regexp.MustCompile(`(?:^|\s)--alias\s+(\S+)`)

// ParseRAM returns system RAM from free -b's output, its Mem: line's total, used and available
// columns, and ps -eo rss=,args='s, each llama-server process with an --alias (a model instance
// the router started; the router itself carries none) by that alias, its RSS in KiB.
func ParseRAM(free, ps []byte) (RAM, error) {
	var ram RAM
	found := false
	for line := range strings.SplitSeq(string(free), "\n") {
		f := strings.Fields(line)
		// Mem: total used free shared buff/cache available
		if len(f) < 7 || f[0] != "Mem:" {
			continue
		}
		cols := make([]int64, 0, 6)
		for _, s := range f[1:7] {
			v, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return RAM{}, fmt.Errorf("free's Mem: line %q: %w", line, err)
			}
			cols = append(cols, v)
		}
		ram.TotalMiB, ram.UsedMiB, ram.AvailableMiB = int(cols[0]>>20), int(cols[1]>>20), int(cols[5]>>20)
		found = true
	}
	if !found {
		return RAM{}, fmt.Errorf("free's output holds no Mem: line")
	}
	for line := range strings.SplitSeq(string(ps), "\n") {
		rss, args, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !strings.Contains(args, "llama-server") {
			continue
		}
		m := aliasArg.FindStringSubmatch(args)
		kib, err := strconv.Atoi(rss)
		if m == nil || err != nil {
			continue
		}
		if ram.RSSMiB == nil {
			ram.RSSMiB = map[string]int{}
		}
		ram.RSSMiB[m[1]] += kib >> 10
	}
	return ram, nil
}

// poolValues are a reading's figures as a check's values: the pool's used and free memory, and
// its system RAM's used, available and each model process's RSS (rss_mib <model>).
func poolValues(p Pool) map[string]float64 {
	v := map[string]float64{"pool_used_mib": float64(p.UsedMiB), "pool_free_mib": float64(p.FreeMiB())}
	if p.RAM != nil {
		v["ram_used_mib"], v["ram_available_mib"] = float64(p.RAM.UsedMiB), float64(p.RAM.AvailableMiB)
		for m, rss := range p.RAM.RSSMiB {
			v["rss_mib "+m] = float64(rss)
		}
	}
	return v
}

// ramDetail is a reading's system RAM for a check's detail, empty for a reading without it.
func ramDetail(p Pool) string {
	if p.RAM == nil {
		return ""
	}
	return fmt.Sprintf("; system RAM %d MiB used, %d available", p.RAM.UsedMiB, p.RAM.AvailableMiB)
}
