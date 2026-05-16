package main

import (
	"encoding/csv"
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

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	payloadSize  = 100
	metricsEvery = 10 * time.Second
	pushEvery    = 5 * time.Second
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

func handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // ローカル測定用に Origin チェックを緩和
	})
	if err != nil {
		log.Printf("ws accept error: %v", err)
		return
	}
	activeConns.Add(1)
	defer activeConns.Add(-1)
	defer c.CloseNow()

	ctx := r.Context()
	ticker := time.NewTicker(pushEvery)
	defer ticker.Stop()

	log.Printf("ws connected: %s", r.RemoteAddr)

	for {
		select {
		case <-ctx.Done():
			_ = c.Close(websocket.StatusNormalClosure, "bye")
			return
		case <-ticker.C:
			msg := Message{
				Seq:       seqCounter.Add(1),
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				Payload:   payload,
			}
			if err := wsjson.Write(ctx, c, &msg); err != nil {
				log.Printf("ws write error: %v", err)
				return
			}
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

	if err := startMetricsCollector("websocket", resultsDir); err != nil {
		log.Fatalf("metrics: %v", err)
	}

	http.HandleFunc("/ws", handleWS)
	log.Printf("websocket server listening on %s (payload=%d bytes, push=%s)", addr, payloadSize, pushEvery)
	log.Fatal(http.ListenAndServe(addr, nil))
}
