package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handlePrometheus serves the cached metrics in the Prometheus/OpenMetrics text
// exposition format at GET /metrics, so an existing Prometheus/Grafana stack can
// scrape bewitch's hardware/ECC/power/SMART/GPU metrics rather than competing
// with it. It reads only the in-memory snapshot (no DB), like the other live
// metric handlers. Series carry a bewitch_ prefix; lifetime _total families and
// uptime are typed as counters, everything else as gauges.
func (s *Server) handlePrometheus(w http.ResponseWriter, r *http.Request) {
	p := &promWriter{}

	if cpu, _ := s.getCachedCPU(); cpu != nil {
		p.help("bewitch_cpu_percent", "Per-core CPU time by mode (core=-1 is the aggregate)")
		for _, c := range cpu {
			core := strconv.Itoa(c.Core)
			p.gauge("bewitch_cpu_percent", c.UserPct, "core", core, "mode", "user")
			p.gauge("bewitch_cpu_percent", c.SystemPct, "core", core, "mode", "system")
			p.gauge("bewitch_cpu_percent", c.IdlePct, "core", core, "mode", "idle")
			p.gauge("bewitch_cpu_percent", c.IOWaitPct, "core", core, "mode", "iowait")
			p.gauge("bewitch_cpu_percent", c.StealPct, "core", core, "mode", "steal")
		}
	}

	if m, _ := s.getCachedMemory(); m != nil {
		p.help("bewitch_memory_bytes", "Memory by kind, in bytes")
		p.gauge("bewitch_memory_bytes", float64(m.TotalBytes), "kind", "total")
		p.gauge("bewitch_memory_bytes", float64(m.UsedBytes), "kind", "used")
		p.gauge("bewitch_memory_bytes", float64(m.AvailableBytes), "kind", "available")
		p.gauge("bewitch_memory_bytes", float64(m.BuffersBytes), "kind", "buffers")
		p.gauge("bewitch_memory_bytes", float64(m.CachedBytes), "kind", "cached")
		p.gauge("bewitch_memory_bytes", float64(m.SwapTotalBytes), "kind", "swap_total")
		p.gauge("bewitch_memory_bytes", float64(m.SwapUsedBytes), "kind", "swap_used")
	}

	if l, _ := s.getCachedLoad(); l != nil {
		p.help("bewitch_load_average", "System load average")
		p.gauge("bewitch_load_average", l.Load1, "period", "1m")
		p.gauge("bewitch_load_average", l.Load5, "period", "5m")
		p.gauge("bewitch_load_average", l.Load15, "period", "15m")
	}

	if disks, _ := s.getCachedDisk(); len(disks) > 0 {
		p.help("bewitch_disk_bytes", "Filesystem space by kind, in bytes")
		for _, d := range disks {
			p.gauge("bewitch_disk_bytes", float64(d.TotalBytes), "mount", d.Mount, "kind", "total")
			p.gauge("bewitch_disk_bytes", float64(d.UsedBytes), "mount", d.Mount, "kind", "used")
			p.gauge("bewitch_disk_bytes", float64(d.FreeBytes), "mount", d.Mount, "kind", "free")
		}
		p.help("bewitch_disk_inodes", "Filesystem inodes by kind")
		for _, d := range disks {
			if d.InodesTotal == 0 {
				continue
			}
			p.gauge("bewitch_disk_inodes", float64(d.InodesTotal), "mount", d.Mount, "kind", "total")
			p.gauge("bewitch_disk_inodes", float64(d.InodesFree), "mount", d.Mount, "kind", "free")
		}
		p.help("bewitch_disk_io_bytes_per_second", "Disk I/O throughput, bytes/sec")
		for _, d := range disks {
			p.gauge("bewitch_disk_io_bytes_per_second", d.ReadBytesSec, "mount", d.Mount, "direction", "read")
			p.gauge("bewitch_disk_io_bytes_per_second", d.WriteBytesSec, "mount", d.Mount, "direction", "write")
		}
		// SMART (live values carried on the disk metric)
		p.help("bewitch_smart_healthy", "SMART overall-health (1 = healthy)")
		p.help("bewitch_smart_temperature_celsius", "SMART reported drive temperature")
		p.help("bewitch_smart_percent_used", "NVMe estimated wear, percent")
		p.help("bewitch_smart_reallocated_sectors", "SMART reallocated sector count")
		for _, d := range disks {
			if !d.SMARTAvailable {
				continue
			}
			p.gauge("bewitch_smart_healthy", boolToFloat(d.SMARTHealthy), "device", d.Device)
			p.gauge("bewitch_smart_temperature_celsius", float64(d.SMARTTemperature), "device", d.Device)
			p.gauge("bewitch_smart_percent_used", float64(d.SMARTPercentUsed), "device", d.Device)
			p.gauge("bewitch_smart_reallocated_sectors", float64(d.SMARTReallocated), "device", d.Device)
		}
	}

	if net, _ := s.getCachedNetwork(); len(net) > 0 {
		p.help("bewitch_network_bytes_per_second", "Network throughput, bytes/sec")
		p.helpTyped("bewitch_network_errors_total", "Network error counter (lifetime)", "counter")
		p.helpTyped("bewitch_network_dropped_total", "Network dropped-packet counter (lifetime)", "counter")
		for _, n := range net {
			p.gauge("bewitch_network_bytes_per_second", n.RxBytesSec, "interface", n.Interface, "direction", "rx")
			p.gauge("bewitch_network_bytes_per_second", n.TxBytesSec, "interface", n.Interface, "direction", "tx")
			p.counter("bewitch_network_errors_total", float64(n.RxErrors), "interface", n.Interface, "direction", "rx")
			p.counter("bewitch_network_errors_total", float64(n.TxErrors), "interface", n.Interface, "direction", "tx")
			p.counter("bewitch_network_dropped_total", float64(n.RxDropped), "interface", n.Interface, "direction", "rx")
			p.counter("bewitch_network_dropped_total", float64(n.TxDropped), "interface", n.Interface, "direction", "tx")
		}
	}

	if temps, _ := s.getCachedTemperature(); len(temps) > 0 {
		p.help("bewitch_temperature_celsius", "Temperature sensor reading")
		for _, t := range temps {
			p.gauge("bewitch_temperature_celsius", t.TempCelsius, "sensor", t.Sensor)
		}
	}

	if power, _ := s.getCachedPower(); len(power) > 0 {
		p.help("bewitch_power_watts", "Power draw by RAPL zone")
		for _, z := range power {
			p.gauge("bewitch_power_watts", z.Watts, "zone", z.Zone)
		}
	}

	if ecc, _ := s.getCachedECC(); ecc != nil {
		p.helpTyped("bewitch_ecc_errors_total", "ECC memory error counter", "counter")
		p.counter("bewitch_ecc_errors_total", float64(ecc.Corrected), "kind", "corrected")
		p.counter("bewitch_ecc_errors_total", float64(ecc.Uncorrected), "kind", "uncorrected")
	}

	if gpus, _ := s.getCachedGPU(); len(gpus) > 0 {
		p.help("bewitch_gpu_utilization_percent", "GPU utilization")
		p.help("bewitch_gpu_temperature_celsius", "GPU temperature")
		p.help("bewitch_gpu_power_watts", "GPU power draw")
		p.help("bewitch_gpu_memory_bytes", "GPU memory by kind")
		for _, g := range gpus {
			p.gauge("bewitch_gpu_utilization_percent", g.UtilizationPct, "gpu", g.Name)
			p.gauge("bewitch_gpu_temperature_celsius", g.TempCelsius, "gpu", g.Name)
			p.gauge("bewitch_gpu_power_watts", g.PowerWatts, "gpu", g.Name)
			if g.MemoryTotalBytes > 0 {
				p.gauge("bewitch_gpu_memory_bytes", float64(g.MemoryUsedBytes), "gpu", g.Name, "kind", "used")
				p.gauge("bewitch_gpu_memory_bytes", float64(g.MemoryTotalBytes), "gpu", g.Name, "kind", "total")
			}
		}
	}

	// Process aggregates only — per-process series would be unbounded cardinality.
	if procs, _ := s.getCachedProcess(); procs != nil {
		p.help("bewitch_processes", "Process counts by state")
		p.gauge("bewitch_processes", float64(procs.TotalProcs), "state", "total")
		p.gauge("bewitch_processes", float64(procs.RunningProcs), "state", "running")
		p.gauge("bewitch_processes", float64(procs.ActiveProcs), "state", "active")
	}

	// Daemon self-health. Uptime is always available (a restart, itself a gap
	// cause, shows up as a counter reset); the rest need the daemon's self-stats
	// provider. The single labeled family (collector_consecutive_fails) is bounded
	// by the fixed collector set, so cardinality stays safe.
	p.helpTyped("bewitch_self_uptime_seconds", "Seconds since the daemon started (resets on restart)", "counter")
	p.counter("bewitch_self_uptime_seconds", time.Since(s.startTime).Seconds())

	if s.selfStatsFn != nil {
		ss := s.selfStatsFn()
		p.helpTyped("bewitch_self_dropped_write_batches_total", "Metric batches dropped because the async write queue was full", "counter")
		p.counter("bewitch_self_dropped_write_batches_total", float64(ss.DroppedWriteBatches))
		p.helpTyped("bewitch_self_pause_dropped_samples_total", "Samples dropped from the pause buffer when it hit its cap during maintenance", "counter")
		p.counter("bewitch_self_pause_dropped_samples_total", float64(ss.PauseDroppedSamples))
		p.help("bewitch_self_proc_info_cache_entries", "Entries in the process-info dedup cache (a documented RSS-growth source)")
		p.gauge("bewitch_self_proc_info_cache_entries", float64(ss.ProcInfoCacheEntries))
		p.help("bewitch_self_write_queue_depth", "Metric batches buffered awaiting the DB writer")
		p.gauge("bewitch_self_write_queue_depth", float64(ss.WriteQueueDepth))
		p.help("bewitch_self_write_queue_capacity", "Capacity of the async write queue")
		p.gauge("bewitch_self_write_queue_capacity", float64(ss.WriteQueueCap))
		p.help("bewitch_self_memory_heap_bytes", "Go heap memory currently allocated")
		p.gauge("bewitch_self_memory_heap_bytes", float64(ss.HeapBytes))
		p.help("bewitch_self_memory_rss_bytes", "Resident set size of the daemon process")
		p.gauge("bewitch_self_memory_rss_bytes", float64(ss.RSSBytes))
		p.help("bewitch_self_goroutines", "Number of goroutines")
		p.gauge("bewitch_self_goroutines", float64(ss.Goroutines))
		p.help("bewitch_self_collector_consecutive_fails", "Consecutive collector failures (0 = healthy; >0 means in exponential backoff)")
		for name, fails := range ss.CollectorFails {
			p.gauge("bewitch_self_collector_consecutive_fails", float64(fails), "collector", name)
		}
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(p.b.String()))
}

// promWriter accumulates Prometheus text-format output, emitting each metric
// family's HELP/TYPE header once.
type promWriter struct {
	b      strings.Builder
	helped map[string]bool
}

// help emits a HELP/TYPE header for a gauge family (the common case).
func (p *promWriter) help(name, help string) {
	p.helpTyped(name, help, "gauge")
}

// helpTyped emits a HELP/TYPE header with an explicit metric type ("gauge" or
// "counter"), once per family. Counters must use this so rate()/increase() are
// valid on _total series — the old hardcoded-"gauge" header silently mislabeled
// every _total family.
func (p *promWriter) helpTyped(name, help, typ string) {
	if p.helped == nil {
		p.helped = make(map[string]bool)
	}
	if p.helped[name] {
		return
	}
	p.helped[name] = true
	fmt.Fprintf(&p.b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

// gauge and counter write identical sample lines; only the TYPE header (emitted
// by help/helpTyped) differs. Two names keep call sites self-documenting.
func (p *promWriter) gauge(name string, value float64, labelKV ...string) {
	p.series(name, value, labelKV...)
}

func (p *promWriter) counter(name string, value float64, labelKV ...string) {
	p.series(name, value, labelKV...)
}

func (p *promWriter) series(name string, value float64, labelKV ...string) {
	p.b.WriteString(name)
	if len(labelKV) > 0 {
		p.b.WriteByte('{')
		for i := 0; i+1 < len(labelKV); i += 2 {
			if i > 0 {
				p.b.WriteByte(',')
			}
			p.b.WriteString(labelKV[i])
			p.b.WriteString(`="`)
			p.b.WriteString(promEscapeLabel(labelKV[i+1]))
			p.b.WriteByte('"')
		}
		p.b.WriteByte('}')
	}
	p.b.WriteByte(' ')
	p.b.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
	p.b.WriteByte('\n')
}

// promEscapeLabel escapes a label value per the exposition format: backslash,
// double-quote, and newline.
func promEscapeLabel(s string) string {
	if !strings.ContainsAny(s, "\\\"\n") {
		return s
	}
	r := strings.NewReplacer("\\", `\\`, "\"", `\"`, "\n", `\n`)
	return r.Replace(s)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
