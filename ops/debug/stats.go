package debug

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

var processStart = time.Now()

// Stats is one point-in-time runtime snapshot, the JSON body of /debug/stats.
type Stats struct {
	GoVersion     string   `json:"go_version"`
	GC            GCStats  `json:"gc"`
	Mem           MemStats `json:"mem"`
	UptimeSeconds float64  `json:"uptime_seconds"`
	Goroutines    int      `json:"goroutines"`
	GOMAXPROCS    int      `json:"gomaxprocs"`
	NumCPU        int      `json:"num_cpu"`
	NumCgoCall    int64    `json:"num_cgo_call"`
}

// MemStats is the curated subset of runtime.MemStats served by /debug/stats.
// All sizes are bytes.
type MemStats struct {
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Sys          uint64 `json:"sys"`
	Mallocs      uint64 `json:"mallocs"`
	Frees        uint64 `json:"frees"`
	HeapAlloc    uint64 `json:"heap_alloc"`
	HeapSys      uint64 `json:"heap_sys"`
	HeapInuse    uint64 `json:"heap_inuse"`
	HeapIdle     uint64 `json:"heap_idle"`
	HeapReleased uint64 `json:"heap_released"`
	HeapObjects  uint64 `json:"heap_objects"`
	StackInuse   uint64 `json:"stack_inuse"`
	StackSys     uint64 `json:"stack_sys"`
}

// GCStats summarizes garbage-collector activity. LastGC/LastPauseNs are zero
// until the first collection.
type GCStats struct {
	LastGC       time.Time `json:"last_gc,omitzero"`
	NextGC       uint64    `json:"next_gc"`
	PauseTotalNs uint64    `json:"pause_total_ns"`
	LastPauseNs  uint64    `json:"last_pause_ns"`
	CPUFraction  float64   `json:"cpu_fraction"`
	NumGC        uint32    `json:"num_gc"`
	NumForcedGC  uint32    `json:"num_forced_gc"`
}

// Snapshot collects the current runtime stats. It calls runtime.ReadMemStats,
// which briefly stops the world — fine for a diagnostics scrape, not for a per-
// request hot path.
func Snapshot() Stats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	gc := GCStats{
		NextGC:       m.NextGC,
		PauseTotalNs: m.PauseTotalNs,
		CPUFraction:  m.GCCPUFraction,
		NumGC:        m.NumGC,
		NumForcedGC:  m.NumForcedGC,
	}
	if m.NumGC > 0 {
		gc.LastGC = time.Unix(0, int64(m.LastGC))
		gc.LastPauseNs = m.PauseNs[(m.NumGC+255)%256]
	}
	return Stats{
		GoVersion:     runtime.Version(),
		UptimeSeconds: time.Since(processStart).Seconds(),
		Goroutines:    runtime.NumGoroutine(),
		GOMAXPROCS:    runtime.GOMAXPROCS(0),
		NumCPU:        runtime.NumCPU(),
		NumCgoCall:    runtime.NumCgoCall(),
		Mem: MemStats{
			Alloc:        m.Alloc,
			TotalAlloc:   m.TotalAlloc,
			Sys:          m.Sys,
			Mallocs:      m.Mallocs,
			Frees:        m.Frees,
			HeapAlloc:    m.HeapAlloc,
			HeapSys:      m.HeapSys,
			HeapInuse:    m.HeapInuse,
			HeapIdle:     m.HeapIdle,
			HeapReleased: m.HeapReleased,
			HeapObjects:  m.HeapObjects,
			StackInuse:   m.StackInuse,
			StackSys:     m.StackSys,
		},
		GC: gc,
	}
}

func statsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Snapshot())
}
