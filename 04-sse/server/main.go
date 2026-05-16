package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	payloadSize  = 100
	metricsEvery = 10 * time.Second
	sendEvery    = 5 * time.Second
	defaultAddr  = ":8080"
)

var (
	seqCounter  atomic.Int64
	activeConns atomic.Int64
	payload     = strings.Repeat("x", payloadSize)
)

type Message struct {
	Seq       int64  `json:"seq"`
	Timestamp string `json:"ts"`
	Payload   string `json:"payload"`
}

func handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	activeConns.Add(1)
	defer activeConns.Add(-1)

	ticker := time.NewTicker(sendEvery)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg := Message{
				Seq:       seqCounter.Add(1),
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				Payload:   payload,
			}
			b, err := json.Marshal(msg)
			if err != nil {
				log.Printf("marshal error: %v", err)
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				log.Printf("write error: %v", err)
				return
			}
			flusher.Flush()
		}
	}
}

func startMetricsCollector(method, resultsDir string) error {
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir results: %w", err)
	}
	ts := time.Now().Format("20060102-150405")
	path := filepath.Join(resultsDir, fmt.Sprintf("server-metrics-%s-%s.csv", method, ts))
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create metrics file: %w", err)
	}

	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"timestamp", "heap_mb", "sys_mb", "goroutines",
		"cpu_user_sec", "cpu_sys_sec", "active_conns",
	}); err != nil {
		return err
	}
	w.Flush()

	go func() {
		defer f.Close()
		ticker := time.NewTicker(metricsEvery)
		defer ticker.Stop()
		for t := range ticker.C {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			var ru syscall.Rusage
			_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
			cpuUser := float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6
			cpuSys := float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
			_ = w.Write([]string{
				t.UTC().Format(time.RFC3339),
				fmt.Sprintf("%.2f", float64(m.HeapAlloc)/1024/1024),
				fmt.Sprintf("%.2f", float64(m.Sys)/1024/1024),
				fmt.Sprintf("%d", runtime.NumGoroutine()),
				fmt.Sprintf("%.3f", cpuUser),
				fmt.Sprintf("%.3f", cpuSys),
				fmt.Sprintf("%d", activeConns.Load()),
			})
			w.Flush()
		}
	}()

	log.Printf("server metrics -> %s", path)
	return nil
}

func main() {
	addr := defaultAddr
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}
	resultsDir := "./results"
	if v := os.Getenv("RESULTS_DIR"); v != "" {
		resultsDir = v
	}

	if err := startMetricsCollector("sse", resultsDir); err != nil {
		log.Fatalf("metrics: %v", err)
	}

	http.HandleFunc("/events", handleEvents)
	log.Printf("sse server listening on %s (payload=%d bytes, send=%s)", addr, payloadSize, sendEvery)
	log.Fatal(http.ListenAndServe(addr, nil))
}
