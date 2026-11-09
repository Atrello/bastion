// The server's health for the panel's cards: CPU, memory and disk, read from /proc and the root disk.
package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/Atrello/bastion/internal/protocol"
)

// The last CPU counters, so each reading is the use since the one before. Read every 10 seconds and
// straight after any game request, which can be at once, hence the lock.
var (
	cpuMu               sync.Mutex
	lastIdle, lastTotal uint64
)

func readMetrics() protocol.Metrics {
	var m protocol.Metrics
	if idle, total, ok := cpuTimes(); ok {
		cpuMu.Lock()
		if lastTotal > 0 && total > lastTotal {
			busy := float64((total-lastTotal)-(idle-lastIdle)) / float64(total-lastTotal)
			m.CPUPercent = float64(int(busy*1000)) / 10
		}
		lastIdle, lastTotal = idle, total
		cpuMu.Unlock()
	}
	if mem := readKeyValues("/proc/meminfo"); mem != nil {
		m.MemTotal = mem["MemTotal"] * 1024
		m.MemUsed = m.MemTotal - mem["MemAvailable"]*1024
	}
	var fs syscall.Statfs_t
	if syscall.Statfs("/", &fs) == nil {
		m.DiskTotal = fs.Blocks * uint64(fs.Bsize)
		m.DiskUsed = m.DiskTotal - fs.Bavail*uint64(fs.Bsize)
	}
	if raw, err := os.ReadFile("/proc/uptime"); err == nil {
		up, _ := strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
		m.UptimeSecs = uint64(up)
	}
	return m
}

func cpuTimes() (idle, total uint64, ok bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(strings.SplitN(string(raw), "\n", 2)[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for i, f := range fields[1:] {
		n, _ := strconv.ParseUint(f, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle and iowait
			idle += n
		}
	}
	return idle, total, true
}

// readKeyValues reads "Name:   1234 kB" lines, as /proc/meminfo has them.
func readKeyValues(path string) map[string]uint64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]uint64{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		name, rest, ok := strings.Cut(scan.Text(), ":")
		if !ok {
			continue
		}
		if fields := strings.Fields(rest); len(fields) > 0 {
			out[name], _ = strconv.ParseUint(fields[0], 10, 64)
		}
	}
	return out
}
