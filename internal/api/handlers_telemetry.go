package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

const defaultTelemetryPoints = 60

type telemetryResponse struct {
	CadenceSeconds int64               `json:"cadence_seconds"`
	Latest         *telemetryPointDTO  `json:"latest"`
	Points         []telemetryPointDTO `json:"points"`
}

type telemetryPointDTO struct {
	Timestamp string   `json:"timestamp"`
	Health    string   `json:"health"`
	Issues    []string `json:"issues"`

	CPUPercent       *float64 `json:"cpu_percent"`
	MemoryUsedBytes  *uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes *uint64  `json:"memory_total_bytes"`
	MemoryPercent    *float64 `json:"memory_percent"`
	DiskUsedBytes    *uint64  `json:"disk_used_bytes"`
	DiskTotalBytes   *uint64  `json:"disk_total_bytes"`
	DiskPercent      *float64 `json:"disk_percent"`
	Load1            *float64 `json:"load_1"`
	UptimeSeconds    *uint64  `json:"uptime_seconds"`

	ProcessRSSBytes  *uint64 `json:"process_rss_bytes"`
	ProcessHeapBytes *uint64 `json:"process_heap_bytes"`
	Goroutines       *uint64 `json:"goroutines"`

	HostRXBytes *uint64 `json:"host_rx_bytes"`
	HostTXBytes *uint64 `json:"host_tx_bytes"`
	VPNRXBytes  *uint64 `json:"vpn_rx_bytes"`
	VPNTXBytes  *uint64 `json:"vpn_tx_bytes"`

	HostRXBytesPerSecond *float64 `json:"host_rx_bytes_per_second"`
	HostTXBytesPerSecond *float64 `json:"host_tx_bytes_per_second"`
	VPNRXBytesPerSecond  *float64 `json:"vpn_rx_bytes_per_second"`
	VPNTXBytesPerSecond  *float64 `json:"vpn_tx_bytes_per_second"`

	OnlineUsers        *uint64 `json:"online_users"`
	ActivePeers        *uint64 `json:"active_peers"`
	EnabledInterfaces  *uint64 `json:"enabled_interfaces"`
	ObservedInterfaces *uint64 `json:"observed_interfaces"`
}

func (s *Server) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	points, err := parseTelemetryPoints(r)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	response := telemetryResponse{
		CadenceSeconds: int64(telemetry.DefaultCadence / time.Second),
		Points:         make([]telemetryPointDTO, 0),
	}
	if s.Telemetry != nil {
		history := s.Telemetry.Snapshot(time.Now().UTC(), points)
		response.CadenceSeconds = int64(history.Cadence / time.Second)
		response.Points = make([]telemetryPointDTO, 0, len(history.Points))
		for _, point := range history.Points {
			response.Points = append(response.Points, toTelemetryPointDTO(point))
		}
		if history.Available {
			latest := toTelemetryPointDTO(history.Latest)
			response.Latest = &latest
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func parseTelemetryPoints(r *http.Request) (int, error) {
	values, exists := r.URL.Query()["points"]
	if !exists {
		return defaultTelemetryPoints, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, invalidRequestErr("points must be one integer between 1 and %d", telemetry.HistoryCapacity)
	}
	points, err := strconv.Atoi(values[0])
	if err != nil || points < 1 {
		return 0, invalidRequestErr("points must be one integer between 1 and %d", telemetry.HistoryCapacity)
	}
	if points > telemetry.HistoryCapacity {
		points = telemetry.HistoryCapacity
	}
	return points, nil
}

func toTelemetryPointDTO(point telemetry.Point) telemetryPointDTO {
	return telemetryPointDTO{
		Timestamp: point.At.UTC().Format(time.RFC3339Nano),
		Health:    string(point.Health),
		Issues:    point.Issues.Codes(),

		CPUPercent:       floatMetric(point.CPUPercent),
		MemoryUsedBytes:  uintMetric(point.MemoryUsedBytes),
		MemoryTotalBytes: uintMetric(point.MemoryTotalBytes),
		MemoryPercent:    floatMetric(point.MemoryPercent),
		DiskUsedBytes:    uintMetric(point.DiskUsedBytes),
		DiskTotalBytes:   uintMetric(point.DiskTotalBytes),
		DiskPercent:      floatMetric(point.DiskPercent),
		Load1:            floatMetric(point.Load1),
		UptimeSeconds:    uintMetric(point.UptimeSeconds),

		ProcessRSSBytes:  uintMetric(point.ProcessRSSBytes),
		ProcessHeapBytes: uintMetric(point.ProcessHeapBytes),
		Goroutines:       uintMetric(point.Goroutines),

		HostRXBytes: uintMetric(point.HostRXBytes),
		HostTXBytes: uintMetric(point.HostTXBytes),
		VPNRXBytes:  uintMetric(point.VPNRXBytes),
		VPNTXBytes:  uintMetric(point.VPNTXBytes),

		HostRXBytesPerSecond: floatMetric(point.HostRXRate),
		HostTXBytesPerSecond: floatMetric(point.HostTXRate),
		VPNRXBytesPerSecond:  floatMetric(point.VPNRXRate),
		VPNTXBytesPerSecond:  floatMetric(point.VPNTXRate),

		OnlineUsers:        uintMetric(point.OnlineUsers),
		ActivePeers:        uintMetric(point.ActivePeers),
		EnabledInterfaces:  uintMetric(point.EnabledInterfaces),
		ObservedInterfaces: uintMetric(point.ObservedInterfaces),
	}
}

func floatMetric(metric telemetry.Metric) *float64 {
	if !metric.Available {
		return nil
	}
	value := metric.Value
	return &value
}

func uintMetric(metric telemetry.UintMetric) *uint64 {
	if !metric.Available {
		return nil
	}
	value := metric.Value
	return &value
}
