//go:build linux || android

package service

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func availableMemory() uint64 {
	file, e := os.Open("/proc/meminfo")
	if e != nil {
		return 0
	}
	defer file.Close()
	var available uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "MemAvailable:" && fields[2] == "kB" {
			n, e := strconv.ParseUint(fields[1], 10, 64)
			if e != nil {
				return 0
			}
			available = n * 1024
			break
		}
	}
	if available == 0 {
		return 0
	}
	// Honor the current cgroup and its parents. A container's view of MemAvailable
	// can describe the whole host rather than its actual memory allowance.
	groups, e := os.ReadFile("/proc/self/cgroup")
	if e != nil {
		return 0
	}
	for _, line := range strings.Split(string(groups), "\n") {
		if !strings.HasPrefix(line, "0::") {
			continue
		}
		relative := strings.TrimPrefix(line, "0::")
		directory := filepath.Join("/sys/fs/cgroup", filepath.Clean("/"+relative))
		for {
			maximum, me := os.ReadFile(filepath.Join(directory, "memory.max"))
			current, ce := os.ReadFile(filepath.Join(directory, "memory.current"))
			if me == nil && ce == nil && strings.TrimSpace(string(maximum)) != "max" {
				limit, le := strconv.ParseUint(strings.TrimSpace(string(maximum)), 10, 64)
				used, ue := strconv.ParseUint(strings.TrimSpace(string(current)), 10, 64)
				if le != nil || ue != nil {
					return 0
				}
				if used >= limit {
					return 1
				}
				available = min(available, limit-used)
			}
			if directory == "/sys/fs/cgroup" {
				break
			}
			parent := filepath.Dir(directory)
			if parent == directory || !strings.HasPrefix(parent, "/sys/fs/cgroup") {
				break
			}
			directory = parent
		}
	}
	return available
}
