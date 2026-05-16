package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	msgCount   atomic.Int64
	bytesCount atomic.Int64
)

type Message struct {
	Seq       int64  `json:"seq"`
	Timestamp string `json:"ts"`
	Payload   string `json:"payload"`
}

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "longpolling server base URL")
	interval := flag.Duration("interval", 0, "minimum gap between requests (0 = reconnect immediately)")
	verbose := flag.Bool("verbose", false, "log every recv (default: log every 12 recv)")
	flag.Parse()

	log.Printf("longpolling client -> %s (min-gap=%s)", *serverURL, *interval)

	start := time.Now()
	done := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		close(done)
	}()

	// Server timeout is 30s; keep client a bit longer to avoid races.
	client := &http.Client{Timeout: 35 * time.Second}

	for {
		select {
		case <-done:
			printSummary(start)
			return
		default:
		}

		ok := pollOnce(client, *serverURL, *verbose)

		if !ok {
			// On error, back off briefly to avoid hot-looping.
			select {
			case <-done:
				printSummary(start)
				return
			case <-time.After(1 * time.Second):
			}
			continue
		}

		if *interval > 0 {
			select {
			case <-done:
				printSummary(start)
				return
			case <-time.After(*interval):
			}
		}
	}
}

// pollOnce returns true on a successful exchange (including 204 No Content),
// false on transport / decode errors so the caller can back off.
func pollOnce(client *http.Client, base string, verbose bool) bool {
	resp, err := client.Get(base + "/poll")
	if err != nil {
		log.Printf("poll error: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		// Server timed out without a broadcast; reconnect immediately.
		return true
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("unexpected status: %d", resp.StatusCode)
		_, _ = io.Copy(io.Discard, resp.Body)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("read error: %v", err)
		return false
	}
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		log.Printf("decode error: %v", err)
		return false
	}
	count := msgCount.Add(1)
	bytesCount.Add(int64(len(body)))
	if verbose || count%12 == 0 {
		log.Printf("recv #%d seq=%d bytes=%d", count, msg.Seq, len(body))
	}
	return true
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
