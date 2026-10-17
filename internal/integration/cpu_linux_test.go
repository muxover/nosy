//go:build nosytest && linux

package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// sampleBrowserCPU watches the scan's browser and every process under it
// until stop is called, then sends their CPU seconds on done. A process that
// lives for less than a sampling interval is counted up to the last time it
// was seen.
func sampleBrowserCPU() (stop func(), done <-chan float64) {
	out := make(chan float64, 1)
	quit := make(chan struct{})
	go func() {
		seen := map[int]float64{}
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			sampleOnce(seen)
			select {
			case <-quit:
				var sum float64
				for _, s := range seen {
					sum += s
				}
				out <- sum
				return
			case <-tick.C:
			}
		}
	}()
	return func() { close(quit) }, out
}

type proc struct {
	ppid int
	cpu  float64
}

const clockTicks = 100 // USER_HZ on Linux

func sampleOnce(seen map[int]float64) {
	procs := map[int]proc{}
	root := 0
	stats, _ := filepath.Glob("/proc/[0-9]*/stat")
	for _, f := range stats {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		pid, _ := strconv.Atoi(filepath.Base(filepath.Dir(f)))
		// fields after the command, which is in parentheses and may hold spaces
		i := bytes.LastIndexByte(b, ')')
		if i < 0 {
			continue
		}
		fields := bytes.Fields(b[i+1:])
		if len(fields) < 15 {
			continue
		}
		ppid, _ := strconv.Atoi(string(fields[1]))
		utime, _ := strconv.ParseFloat(string(fields[11]), 64)
		stime, _ := strconv.ParseFloat(string(fields[12]), 64)
		procs[pid] = proc{ppid: ppid, cpu: (utime + stime) / clockTicks}
		if cmd, err := os.ReadFile(filepath.Join(filepath.Dir(f), "cmdline")); err == nil &&
			bytes.Contains(cmd, []byte("nosy-profile-")) && !bytes.Contains(cmd, []byte("--type=")) {
			root = pid
		}
	}
	if root == 0 {
		return
	}
	for pid, p := range procs {
		for a, hops := pid, 0; a > 1 && hops < 32; a, hops = procs[a].ppid, hops+1 {
			if a == root {
				if p.cpu > seen[pid] {
					seen[pid] = p.cpu
				}
				break
			}
		}
	}
}
