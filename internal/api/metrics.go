package api

import (
	"database/sql"
	"net/http"
	"time"
)

// Shared response types used by both API handlers and TUI client.

type CPUCoreMetric struct {
	Core      int     `json:"core"`
	UserPct   float64 `json:"user_pct"`
	SystemPct float64 `json:"system_pct"`
	IdlePct   float64 `json:"idle_pct"`
	IOWaitPct float64 `json:"iowait_pct"`
}

type MemoryMetric struct {
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	BuffersBytes   uint64 `json:"buffers_bytes"`
	CachedBytes    uint64 `json:"cached_bytes"`
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	SwapUsedBytes  uint64 `json:"swap_used_bytes"`
}

type DiskMetric struct {
	Mount         string  `json:"mount"`
	Device        string  `json:"device"`
	Transport     string  `json:"transport,omitempty"` // "nvme", "sata", "usb", "virtio", "mmc", "scsi"
	TotalBytes    uint64  `json:"total_bytes"`
	UsedBytes     uint64  `json:"used_bytes"`
	FreeBytes     uint64  `json:"free_bytes"`
	ReadBytesSec  float64 `json:"read_bytes_sec"`
	WriteBytesSec float64 `json:"write_bytes_sec"`
	ReadIOPS      float64 `json:"read_iops"`
	WriteIOPS     float64 `json:"write_iops"`
	// SMART attributes (live-only; not stored in DB)
	SMARTAvailable      bool   `json:"smart_available"`
	SMARTHealthy        bool   `json:"smart_healthy,omitempty"`
	SMARTTemperature    uint64 `json:"smart_temperature,omitempty"`
	SMARTPowerOnHours   uint64 `json:"smart_power_on_hours,omitempty"`
	SMARTPowerCycles    uint64 `json:"smart_power_cycles,omitempty"`
	SMARTReadSectors    uint64 `json:"smart_read_sectors,omitempty"`
	SMARTWrittenSectors uint64 `json:"smart_written_sectors,omitempty"`
	SMARTReallocated    uint64 `json:"smart_reallocated,omitempty"`
	SMARTPending        uint64 `json:"smart_pending,omitempty"`
	SMARTUncorrectable  uint64 `json:"smart_uncorrectable,omitempty"`
	SMARTReadErrorRate  uint64 `json:"smart_read_error_rate,omitempty"`
	SMARTAvailableSpare uint8  `json:"smart_available_spare,omitempty"`
	SMARTPercentUsed    uint8  `json:"smart_percent_used,omitempty"`
}

type NetworkMetric struct {
	Interface    string  `json:"interface"`
	RxBytesSec   float64 `json:"rx_bytes_sec"`
	TxBytesSec   float64 `json:"tx_bytes_sec"`
	RxPacketsSec float64 `json:"rx_packets_sec"`
	TxPacketsSec float64 `json:"tx_packets_sec"`
	RxErrors     uint64  `json:"rx_errors"`
	TxErrors     uint64  `json:"tx_errors"`
}

type TemperatureMetric struct {
	Sensor      string  `json:"sensor"`
	TempCelsius float64 `json:"temp_celsius"`
}

type PowerMetric struct {
	Zone  string  `json:"zone"`
	Watts float64 `json:"watts"`
}

type ECCMetric struct {
	Corrected   uint64 `json:"corrected"`
	Uncorrected uint64 `json:"uncorrected"`
}

type GPUMetric struct {
	Name             string  `json:"name"`
	Index            int     `json:"index"`
	Vendor           string  `json:"vendor"`
	UtilizationPct   float64 `json:"utilization_pct"`
	MemoryUsedBytes  uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes uint64  `json:"memory_total_bytes"`
	TempCelsius      float64 `json:"temp_celsius"`
	PowerWatts       float64 `json:"power_watts"`
	FrequencyMHz     uint32  `json:"frequency_mhz"`
	FrequencyMaxMHz  uint32  `json:"frequency_max_mhz"`
	ThrottlePct      float64 `json:"throttle_pct"`
}

type ProcessMetric struct {
	PID          int32   `json:"pid"`
	PPID         int32   `json:"ppid"`
	Name         string  `json:"name"`
	Cmdline      string  `json:"cmdline"`
	State        string  `json:"state"`
	UID          uint32  `json:"uid"`
	CPUUserPct   float64 `json:"cpu_user_pct"`
	CPUSystemPct float64 `json:"cpu_system_pct"`
	RSSBytes     uint64  `json:"rss_bytes"`
	VSSBytes     uint64  `json:"vss_bytes"`
	SharedBytes  uint64  `json:"shared_bytes"`
	SwapBytes    uint64  `json:"swap_bytes"`
	NumFDs       int32   `json:"num_fds"`
	NumThreads   int32   `json:"num_threads"`
	StartTimeNs  int64   `json:"start_time_ns"`
	Enriched     bool    `json:"enriched"` // true = full data (Phase 2), false = lightweight (Phase 1 only)
}

type ProcessResponse struct {
	Processes     []ProcessMetric `json:"processes"`
	TotalProcs    int32           `json:"total_procs"`
	RunningProcs  int32           `json:"running_procs"`
	ActiveProcs   int32           `json:"active_procs"`
	TotalCPUPct   float64         `json:"total_cpu_pct"`
	TotalRSSBytes uint64          `json:"total_rss_bytes"`
	EnrichedCount int32           `json:"enriched_count"` // number of fully enriched (Phase 2) processes
}

type AlertMetric struct {
	ID           int       `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	RuleName     string    `json:"rule_name"`
	Severity     string    `json:"severity"`
	Message      string    `json:"message"`
	Acknowledged bool      `json:"acknowledged"`
}

type AlertRuleMetric struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Enabled  bool   `json:"enabled"`
	// Threshold fields
	Metric        string  `json:"metric,omitempty"`
	Operator      string  `json:"operator,omitempty"`
	Value         float64 `json:"value,omitempty"`
	Duration      string  `json:"duration,omitempty"`
	Mount         string  `json:"mount,omitempty"`
	InterfaceName string  `json:"interface_name,omitempty"`
	Sensor        string  `json:"sensor,omitempty"`
	// Predictive fields
	PredictHours int     `json:"predict_hours,omitempty"`
	ThresholdPct float64 `json:"threshold_pct,omitempty"`
	// Variance fields
	DeltaThreshold float64 `json:"delta_threshold,omitempty"`
	MinCount       int     `json:"min_count,omitempty"`
	// Process fields
	ProcessName      string `json:"process_name,omitempty"`
	ProcessPattern   string `json:"process_pattern,omitempty"`
	MinInstances     int    `json:"min_instances,omitempty"`
	RestartThreshold int    `json:"restart_threshold,omitempty"`
	RestartWindow    string `json:"restart_window,omitempty"`
	CheckDuration    string `json:"check_duration,omitempty"`
}

type DashboardData struct {
	CPU         []CPUCoreMetric    `json:"cpu"`
	Memory      *MemoryMetric      `json:"memory,omitempty"`
	Disks       []DiskMetric       `json:"disks"`
	Network     []NetworkMetric    `json:"network"`
	Temperature []TemperatureMetric `json:"temperature"`
	Power       []PowerMetric      `json:"power"`
	GPU         []GPUMetric        `json:"gpu,omitempty"`
	Processes   *ProcessResponse   `json:"processes,omitempty"`
}

// Handlers

func (s *Server) handleMetricsCPU(w http.ResponseWriter, r *http.Request) {
	if cpu, gen := s.getCachedCPU(); cpu != nil {
		serveCached(w, r, CPUResponse{Cores: cpu}, gen)
		return
	}
	rows, err := s.dbFn().Query("SELECT core, user_pct, system_pct, idle_pct, iowait_pct FROM cpu_metrics WHERE ts = (SELECT MAX(ts) FROM cpu_metrics) ORDER BY core")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var cores []CPUCoreMetric
	for rows.Next() {
		var c CPUCoreMetric
		var userPct, sysPct, idlePct, iowaitPct sql.NullFloat64
		rows.Scan(&c.Core, &userPct, &sysPct, &idlePct, &iowaitPct)
		c.UserPct = nf(userPct)
		c.SystemPct = nf(sysPct)
		c.IdlePct = nf(idlePct)
		c.IOWaitPct = nf(iowaitPct)
		cores = append(cores, c)
	}
	if cores == nil {
		cores = []CPUCoreMetric{}
	}
	writeJSON(w, http.StatusOK, CPUResponse{Cores: cores})
}

func (s *Server) handleMetricsMemory(w http.ResponseWriter, r *http.Request) {
	if mem, gen := s.getCachedMemory(); mem != nil {
		serveCached(w, r, mem, gen)
		return
	}
	var m MemoryMetric
	err := s.dbFn().QueryRow("SELECT total_bytes, used_bytes, available_bytes, buffers_bytes, cached_bytes, swap_total_bytes, swap_used_bytes FROM memory_metrics WHERE ts = (SELECT MAX(ts) FROM memory_metrics)").
		Scan(&m.TotalBytes, &m.UsedBytes, &m.AvailableBytes, &m.BuffersBytes, &m.CachedBytes, &m.SwapTotalBytes, &m.SwapUsedBytes)
	if err != nil {
		writeJSON(w, http.StatusOK, &MemoryMetric{})
		return
	}
	writeJSON(w, http.StatusOK, &m)
}

func (s *Server) handleMetricsDisk(w http.ResponseWriter, r *http.Request) {
	if disks, gen := s.getCachedDisk(); disks != nil {
		serveCached(w, r, DiskResponse{Disks: disks}, gen)
		return
	}
	rows, err := s.dbFn().Query(`SELECT dm.value, dd.value, m.total_bytes, m.used_bytes, m.free_bytes,
		m.read_bytes_sec, m.write_bytes_sec, m.read_iops, m.write_iops
		FROM disk_metrics m
		JOIN dimension_values dm ON dm.category = 'mount' AND dm.id = m.mount_id
		LEFT JOIN dimension_values dd ON dd.category = 'device' AND dd.id = m.device_id
		WHERE m.ts = (SELECT MAX(ts) FROM disk_metrics)`)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var disks []DiskMetric
	for rows.Next() {
		var d DiskMetric
		var device sql.NullString
		rows.Scan(&d.Mount, &device, &d.TotalBytes, &d.UsedBytes, &d.FreeBytes, &d.ReadBytesSec, &d.WriteBytesSec, &d.ReadIOPS, &d.WriteIOPS)
		if device.Valid {
			d.Device = device.String
		}
		disks = append(disks, d)
	}
	if disks == nil {
		disks = []DiskMetric{}
	}
	writeJSON(w, http.StatusOK, DiskResponse{Disks: disks})
}

func (s *Server) handleMetricsNetwork(w http.ResponseWriter, r *http.Request) {
	if net, gen := s.getCachedNetwork(); net != nil {
		serveCached(w, r, NetworkResponse{Interfaces: net}, gen)
		return
	}
	rows, err := s.dbFn().Query(`SELECT d.value, m.rx_bytes_sec, m.tx_bytes_sec, m.rx_packets_sec, m.tx_packets_sec, m.rx_errors, m.tx_errors
		FROM network_metrics m
		JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
		WHERE m.ts = (SELECT MAX(ts) FROM network_metrics)
		ORDER BY d.value`)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var ifaces []NetworkMetric
	for rows.Next() {
		var n NetworkMetric
		rows.Scan(&n.Interface, &n.RxBytesSec, &n.TxBytesSec, &n.RxPacketsSec, &n.TxPacketsSec, &n.RxErrors, &n.TxErrors)
		ifaces = append(ifaces, n)
	}
	if ifaces == nil {
		ifaces = []NetworkMetric{}
	}
	writeJSON(w, http.StatusOK, NetworkResponse{Interfaces: ifaces})
}

func (s *Server) handleMetricsTemperature(w http.ResponseWriter, r *http.Request) {
	if temps, gen := s.getCachedTemperature(); temps != nil {
		serveCached(w, r, TemperatureResponse{Sensors: temps}, gen)
		return
	}
	rows, err := s.dbFn().Query(`SELECT d.value, m.temp_celsius
		FROM temperature_metrics m
		JOIN dimension_values d ON d.category = 'sensor' AND d.id = m.sensor_id
		WHERE m.ts = (SELECT MAX(ts) FROM temperature_metrics)
		ORDER BY d.value`)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var temps []TemperatureMetric
	for rows.Next() {
		var t TemperatureMetric
		rows.Scan(&t.Sensor, &t.TempCelsius)
		temps = append(temps, t)
	}
	if temps == nil {
		temps = []TemperatureMetric{}
	}
	writeJSON(w, http.StatusOK, TemperatureResponse{Sensors: temps})
}

func (s *Server) handleMetricsPower(w http.ResponseWriter, r *http.Request) {
	if power, gen := s.getCachedPower(); power != nil {
		serveCached(w, r, PowerResponse{Zones: power}, gen)
		return
	}
	rows, err := s.dbFn().Query(`SELECT d.value, m.watts
		FROM power_metrics m
		JOIN dimension_values d ON d.category = 'zone' AND d.id = m.zone_id
		WHERE m.ts = (SELECT MAX(ts) FROM power_metrics)
		ORDER BY d.value`)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var zones []PowerMetric
	for rows.Next() {
		var p PowerMetric
		rows.Scan(&p.Zone, &p.Watts)
		zones = append(zones, p)
	}
	if zones == nil {
		zones = []PowerMetric{}
	}
	writeJSON(w, http.StatusOK, PowerResponse{Zones: zones})
}

func (s *Server) handleMetricsDashboard(w http.ResponseWriter, r *http.Request) {
	if dash, gen := s.getCachedDashboard(); dash != nil {
		serveCached(w, r, dash, gen)
		return
	}
	dash := DashboardData{}

	// CPU
	cpuRows, err := s.dbFn().Query("SELECT core, user_pct, system_pct, idle_pct, iowait_pct FROM cpu_metrics WHERE ts = (SELECT MAX(ts) FROM cpu_metrics) ORDER BY core")
	if err == nil {
		defer cpuRows.Close()
		for cpuRows.Next() {
			var c CPUCoreMetric
			var userPct, sysPct, idlePct, iowaitPct sql.NullFloat64
			cpuRows.Scan(&c.Core, &userPct, &sysPct, &idlePct, &iowaitPct)
			c.UserPct = nf(userPct)
			c.SystemPct = nf(sysPct)
			c.IdlePct = nf(idlePct)
			c.IOWaitPct = nf(iowaitPct)
			dash.CPU = append(dash.CPU, c)
		}
	}
	if dash.CPU == nil {
		dash.CPU = []CPUCoreMetric{}
	}

	// Memory
	var m MemoryMetric
	if err := s.dbFn().QueryRow("SELECT total_bytes, used_bytes, available_bytes, buffers_bytes, cached_bytes, swap_total_bytes, swap_used_bytes FROM memory_metrics WHERE ts = (SELECT MAX(ts) FROM memory_metrics)").
		Scan(&m.TotalBytes, &m.UsedBytes, &m.AvailableBytes, &m.BuffersBytes, &m.CachedBytes, &m.SwapTotalBytes, &m.SwapUsedBytes); err == nil {
		dash.Memory = &m
	}

	// Disk
	diskRows, err := s.dbFn().Query(`SELECT dm.value, m.total_bytes, m.used_bytes
		FROM disk_metrics m
		JOIN dimension_values dm ON dm.category = 'mount' AND dm.id = m.mount_id
		WHERE m.ts = (SELECT MAX(ts) FROM disk_metrics)`)
	if err == nil {
		defer diskRows.Close()
		for diskRows.Next() {
			var d DiskMetric
			diskRows.Scan(&d.Mount, &d.TotalBytes, &d.UsedBytes)
			dash.Disks = append(dash.Disks, d)
		}
	}
	if dash.Disks == nil {
		dash.Disks = []DiskMetric{}
	}

	// Network
	netRows, err := s.dbFn().Query(`SELECT d.value, m.rx_bytes_sec, m.tx_bytes_sec
		FROM network_metrics m
		JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
		WHERE m.ts = (SELECT MAX(ts) FROM network_metrics)
		ORDER BY d.value`)
	if err == nil {
		defer netRows.Close()
		for netRows.Next() {
			var n NetworkMetric
			netRows.Scan(&n.Interface, &n.RxBytesSec, &n.TxBytesSec)
			dash.Network = append(dash.Network, n)
		}
	}
	if dash.Network == nil {
		dash.Network = []NetworkMetric{}
	}

	// Temperature
	tempRows, err := s.dbFn().Query(`SELECT d.value, m.temp_celsius
		FROM temperature_metrics m
		JOIN dimension_values d ON d.category = 'sensor' AND d.id = m.sensor_id
		WHERE m.ts = (SELECT MAX(ts) FROM temperature_metrics)
		ORDER BY d.value`)
	if err == nil {
		defer tempRows.Close()
		for tempRows.Next() {
			var t TemperatureMetric
			tempRows.Scan(&t.Sensor, &t.TempCelsius)
			dash.Temperature = append(dash.Temperature, t)
		}
	}
	if dash.Temperature == nil {
		dash.Temperature = []TemperatureMetric{}
	}

	// Power
	powerRows, err := s.dbFn().Query(`SELECT d.value, m.watts
		FROM power_metrics m
		JOIN dimension_values d ON d.category = 'zone' AND d.id = m.zone_id
		WHERE m.ts = (SELECT MAX(ts) FROM power_metrics)
		ORDER BY d.value`)
	if err == nil {
		defer powerRows.Close()
		for powerRows.Next() {
			var p PowerMetric
			powerRows.Scan(&p.Zone, &p.Watts)
			dash.Power = append(dash.Power, p)
		}
	}
	if dash.Power == nil {
		dash.Power = []PowerMetric{}
	}

	// Processes - Top 5 by CPU
	procRows, err := s.dbFn().Query(`SELECT pm.pid, pi.name, pm.state,
		pm.cpu_user_pct, pm.cpu_system_pct, pm.rss_bytes
		FROM process_metrics pm
		JOIN process_info pi ON pm.pid = pi.pid AND pm.start_time = pi.start_time
		WHERE pm.ts = (SELECT MAX(ts) FROM process_metrics)
		ORDER BY pm.cpu_user_pct + pm.cpu_system_pct DESC
		LIMIT 5`)
	if err == nil {
		defer procRows.Close()
		var procs []ProcessMetric
		var totalCPU float64
		var totalRSS uint64
		var running int32
		var active int32
		for procRows.Next() {
			var p ProcessMetric
			var state sql.NullString
			procRows.Scan(&p.PID, &p.Name, &state, &p.CPUUserPct, &p.CPUSystemPct, &p.RSSBytes)
			if state.Valid {
				p.State = state.String
				if p.State == "R" {
					running++
				}
			}
			if p.CPUUserPct+p.CPUSystemPct > 0 {
				active++
			}
			totalCPU += p.CPUUserPct + p.CPUSystemPct
			totalRSS += p.RSSBytes
			procs = append(procs, p)
		}
		// Get total count from latest snapshot
		var total int32
		s.dbFn().QueryRow("SELECT COUNT(*) FROM process_metrics WHERE ts = (SELECT MAX(ts) FROM process_metrics)").Scan(&total)
		dash.Processes = &ProcessResponse{
			Processes:     procs,
			TotalProcs:    total,
			RunningProcs:  running,
			ActiveProcs:   active,
			TotalCPUPct:   totalCPU,
			TotalRSSBytes: totalRSS,
		}
	}

	writeJSON(w, http.StatusOK, &dash)
}

func (s *Server) handleMetricsECC(w http.ResponseWriter, r *http.Request) {
	if ecc, gen := s.getCachedECC(); ecc != nil {
		serveCached(w, r, ECCResponse{ECC: ecc}, gen)
		return
	}
	var m ECCMetric
	err := s.dbFn().QueryRow("SELECT corrected, uncorrected FROM ecc_metrics WHERE ts = (SELECT MAX(ts) FROM ecc_metrics)").
		Scan(&m.Corrected, &m.Uncorrected)
	if err != nil {
		writeJSON(w, http.StatusOK, ECCResponse{ECC: &ECCMetric{}})
		return
	}
	writeJSON(w, http.StatusOK, ECCResponse{ECC: &m})
}

func (s *Server) handleMetricsGPU(w http.ResponseWriter, r *http.Request) {
	if gpus, gen := s.getCachedGPU(); gpus != nil {
		serveCached(w, r, GPUResponse{GPUs: gpus, Hints: s.gpuHints}, gen)
		return
	}
	rows, err := s.dbFn().Query(`SELECT d.value, m.utilization_pct, m.memory_used_bytes, m.memory_total_bytes,
		m.temp_celsius, m.power_watts, m.frequency_mhz, m.frequency_max_mhz, m.throttle_pct
		FROM gpu_metrics m
		JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
		WHERE m.ts = (SELECT MAX(ts) FROM gpu_metrics)
		ORDER BY d.value`)
	if err != nil {
		writeJSON(w, http.StatusOK, GPUResponse{GPUs: []GPUMetric{}, Hints: s.gpuHints})
		return
	}
	defer rows.Close()

	var result []GPUMetric
	idx := 0
	for rows.Next() {
		var g GPUMetric
		rows.Scan(&g.Name, &g.UtilizationPct, &g.MemoryUsedBytes, &g.MemoryTotalBytes,
			&g.TempCelsius, &g.PowerWatts, &g.FrequencyMHz, &g.FrequencyMaxMHz, &g.ThrottlePct)
		g.Index = idx
		idx++
		result = append(result, g)
	}
	if result == nil {
		result = []GPUMetric{}
	}
	writeJSON(w, http.StatusOK, GPUResponse{GPUs: result, Hints: s.gpuHints})
}

func (s *Server) handleMetricsProcess(w http.ResponseWriter, r *http.Request) {
	// Serve the live process snapshot (all processes from collector, not just DB-stored top N).
	snap, gen := s.getCachedProcess()
	if snap == nil {
		writeJSON(w, http.StatusOK, &ProcessResponse{})
		return
	}
	serveCached(w, r, snap, gen)
}

func nf(n sql.NullFloat64) float64 {
	if n.Valid {
		return n.Float64
	}
	return 0
}

func ni(n sql.NullInt64) int64 {
	if n.Valid {
		return n.Int64
	}
	return 0
}

func ns(n sql.NullString) string {
	if n.Valid {
		return n.String
	}
	return ""
}
