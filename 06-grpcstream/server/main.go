package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/y-hashisaka/try-notification-golang/06-grpcstream/notifypb"
)

const (
	payloadSize  = 100
	metricsEvery = 10 * time.Second
	pushEvery    = 5 * time.Second
	defaultAddr  = ":50051"
)

var (
	seqCounter  atomic.Int64
	activeConns atomic.Int64
	payload     = strings.Repeat("x", payloadSize)
)

type server struct {
	notifypb.UnimplementedNotifierServer
}

func (s *server) Stream(_ *notifypb.StreamRequest, srv notifypb.Notifier_StreamServer) error {
	activeConns.Add(1)
	defer activeConns.Add(-1)

	log.Printf("grpc stream connected")

	ticker := time.NewTicker(pushEvery)
	defer ticker.Stop()

	ctx := srv.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			msg := &notifypb.Message{
				Seq:     seqCounter.Add(1),
				Ts:      time.Now().UTC().Format(time.RFC3339Nano),
				Payload: payload,
			}
			if err := srv.Send(msg); err != nil {
				log.Printf("grpc send error: %v", err)
				return err
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

	if err := startMetricsCollector("grpcstream", resultsDir); err != nil {
		log.Fatalf("metrics: %v", err)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	s := grpc.NewServer()
	notifypb.RegisterNotifierServer(s, &server{})
	log.Printf("grpc-stream server listening on %s (payload=%d bytes, push=%s)", addr, payloadSize, pushEvery)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
