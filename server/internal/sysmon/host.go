package sysmon

import (
	"context"
	"os"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Host struct {
	CPU         *float64 `json:"cpuPercent,omitempty"`
	MemoryUsed  *uint64  `json:"memoryUsedBytes,omitempty"`
	MemoryTotal *uint64  `json:"memoryTotalBytes,omitempty"`
	ProcessRSS  *uint64  `json:"processRssBytes,omitempty"`
}
type Mount struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	Path      string  `json:"mount"`
	Available bool    `json:"available"`
	Used      *uint64 `json:"usedBytes,omitempty"`
	Total     *uint64 `json:"totalBytes,omitempty"`
	Free      *uint64 `json:"availableBytes,omitempty"`
}
type Sampler struct {
	last     *cpu.TimesStat
	lastTime time.Time
}

// The caller serializes samples. A new process or an unsupported platform
// starts with no percentage until two genuine, increasing readings exist.
func (s *Sampler) Host(ctx context.Context) Host {
	var out Host
	now := time.Now()
	if times, err := cpu.TimesWithContext(ctx, false); err == nil && len(times) == 1 {
		cur := times[0]
		busy := func(v cpu.TimesStat) float64 { return v.User + v.Nice + v.System + v.Irq + v.Softirq + v.Steal }
		if s.last != nil && now.Sub(s.lastTime) > 0 {
			active := busy(cur) - busy(*s.last)
			idle := cur.Idle + cur.Iowait - s.last.Idle - s.last.Iowait
			if active >= 0 && idle >= 0 && active+idle > 0 {
				pct := 100 * active / (active + idle)
				out.CPU = &pct
			}
		}
		s.last = &cur
		s.lastTime = now
	}
	if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil && memory.Total > 0 {
		out.MemoryUsed = &memory.Used
		out.MemoryTotal = &memory.Total
	}
	if p, err := process.NewProcessWithContext(ctx, int32(os.Getpid())); err == nil {
		if m, err := p.MemoryInfoWithContext(ctx); err == nil {
			out.ProcessRSS = &m.RSS
		}
	}
	return out
}
func ReadMount(ctx context.Context, id, label, path string) Mount {
	m := Mount{ID: id, Label: label, Path: path}
	if info, err := disk.UsageWithContext(ctx, path); err == nil && info.Total > 0 {
		m.Available = true
		m.Used = &info.Used
		m.Total = &info.Total
		m.Free = &info.Free
	}
	return m
}
