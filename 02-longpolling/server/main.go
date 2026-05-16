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
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	payloadSize    = 100
	metricsEvery   = 10 * time.Second
	defaultAddr    = ":8080"
	broadcastEvery = 5 * time.Second
	pollTimeout    = 30 * time.Second
)

var (
	seqCounter  atomic.Int64
	activeConns atomic.Int64
	payload     = strings.Repeat("x", payloadSize)

	subsMu sync.Mutex
	subs   = make(map[chan Message]struct{})
)

type Message struct {
	Seq       int64  `json:"seq"`
	Timestamp string `json:"ts"`
	Payload   string `json:"payload"`
}

func subscribe() chan Message {
	ch := make(chan Message, 1)
	subsMu.Lock()
	subs[ch] = struct{}{}
	subsMu.Unlock()
	return ch
}

func unsubscribe(ch chan Message) {
	subsMu.Lock()
	delete(subs, ch)
	subsMu.Unlock()
}

func startBroadcaster() {
	go func() {
		ticker := time.NewTicker(broadcastEvery)
		defer ticker.Stop()
		for t := range ticker.C {
			msg := Message{
				Seq:       seqCounter.Add(1),
				Timestamp: t.UTC().Format(time.RFC3339Nano),
				Payload:   payload,
			}
			subsMu.Lock()
			for ch := range subs {
				select {
				case ch <- msg:
				default:
					// receiver slow / not ready; skip this tick for them
				}
			}
			subsMu.Unlock()
		}
	}()
}

func handlePoll(w http.ResponseWriter, r *http.Request) {
	activeConns.Add(1)
	defer activeConns.Add(-1)

	ch := subscribe()
	defer unsubscribe(ch)

	ctx := r.Context()
	timer := time.NewTimer(pollTimeout)
	defer timer.Stop()

	select {
	case msg := <-ch:
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(msg); err != nil {
			log.Printf("encode error: %v", err)
		}
	case <-timer.C:
		// no broadcast within timeout window
		w.WriteHeader(http.StatusNoContent)
	case <-ctx.Done():
		// client gone
		return
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

	if err := startMetricsCollector("longpolling", resultsDir); err != nil {
		log.Fatalf("metrics: %v", err)
	}

	startBroadcaster()

	http.HandleFunc("/poll", handlePoll)
	log.Printf("longpolling server listening on %s (payload=%d bytes, broadcast=%s, timeout=%s)",
		addr, payloadSize, broadcastEvery, pollTimeout)
	log.Fatal(http.ListenAndServe(addr, nil))
}
