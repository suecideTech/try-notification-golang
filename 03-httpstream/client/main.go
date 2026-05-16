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
	"sync/atomic"
	"syscall"
	"time"
)

var (
	msgCount    atomic.Int64
	bytesCount  atomic.Int64
	reconnects  atomic.Int64
)

type Message struct {
	Seq       int64  `json:"seq"`
	Timestamp string `json:"ts"`
	Payload   string `json:"payload"`
}

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "httpstream server base URL")
	_ = flag.Duration("interval", 5*time.Second, "unused on client (server controls cadence)")
	verbose := flag.Bool("verbose", false, "log every recv (default: log every 12 recv)")
	flag.Parse()

	log.Printf("httpstream client -> %s", *serverURL)

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	client := &http.Client{Timeout: 0}
	url := *serverURL + "/stream"

	for {
		if ctx.Err() != nil {
			printSummary(start)
			return
		}
		err := streamOnce(ctx, client, url, *verbose)
		if ctx.Err() != nil {
			printSummary(start)
			return
		}
		if err != nil {
			log.Printf("stream error: %v (reconnecting in 1s)", err)
		} else {
			log.Printf("stream closed by server (reconnecting in 1s)")
		}
		reconnects.Add(1)
		select {
		case <-ctx.Done():
			printSummary(start)
			return
		case <-time.After(1 * time.Second):
		}
	}
}

func streamOnce(ctx context.Context, client *http.Client, url string, verbose bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("decode error: %v", err)
			continue
		}
		count := msgCount.Add(1)
		bytesCount.Add(int64(len(line)))
		if verbose || count%12 == 0 {
			log.Printf("recv #%d seq=%d bytes=%d", count, msg.Seq, len(line))
		}
	}
	return scanner.Err()
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
	fmt.Printf("reconnects:     %d\n", reconnects.Load())
	fmt.Printf("cpu user (sec): %.3f\n", cpuUser)
	fmt.Printf("cpu sys  (sec): %.3f\n", cpuSys)
	fmt.Printf("heap (MB):      %.2f\n", float64(m.HeapAlloc)/1024/1024)
}
