package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	msgCount   atomic.Int64
	bytesCount atomic.Int64
)

type Message struct {
	Seq       int64  `json:"seq"`
	Timestamp string `json:"ts"`
	Payload   string `json:"payload"`
}

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "sse server base URL")
	verbose := flag.Bool("verbose", false, "log every recv (default: log every 12 recv)")
	flag.Parse()

	log.Printf("sse client -> %s/events", *serverURL)

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	for {
		if ctx.Err() != nil {
			break
		}
		runStream(ctx, *serverURL, *verbose)
		if ctx.Err() != nil {
			break
		}
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
		}
	}
	printSummary(start)
}

func runStream(ctx context.Context, base string, verbose bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/events", nil)
	if err != nil {
		log.Printf("request build error: %v", err)
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("connect error: %v", err)
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("unexpected status: %d", resp.StatusCode)
		return
	}
	log.Printf("connected to %s/events", base)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			log.Printf("decode error: %v", err)
			continue
		}
		count := msgCount.Add(1)
		bytesCount.Add(int64(len(data)))
		if verbose || count%12 == 0 {
			log.Printf("recv #%d seq=%d bytes=%d", count, msg.Seq, len(data))
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		log.Printf("scan error: %v", err)
	}
}

func printSummary(start time.Time) {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	cpuUser := float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6
	cpuSys := float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	elapsed := time.Since(start)
	fmt.Println()
	fmt.Println("=== summary ===")
	fmt.Printf("elapsed:        %s\n", elapsed.Round(time.Second))
	fmt.Printf("messages:       %d\n", msgCount.Load())
	fmt.Printf("bytes:          %d\n", bytesCount.Load())
	fmt.Printf("cpu user (sec): %.3f\n", cpuUser)
	fmt.Printf("cpu sys  (sec): %.3f\n", cpuSys)
	fmt.Printf("heap (MB):      %.2f\n", float64(m.HeapAlloc)/1024/1024)
}
