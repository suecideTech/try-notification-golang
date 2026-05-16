package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
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
	serverURL := flag.String("server", "ws://localhost:8080", "websocket server URL (http:// or ws:// 受付)")
	verbose := flag.Bool("verbose", false, "log every recv (default: log every 12 recv)")
	flag.Parse()

	wsURL, err := normalizeURL(*serverURL)
	if err != nil {
		log.Fatalf("invalid -server: %v", err)
	}

	log.Printf("websocket client -> %s", wsURL)

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
			printSummary(start)
			return
		}
		if err := runOnce(ctx, wsURL, *verbose); err != nil {
			if ctx.Err() != nil {
				printSummary(start)
				return
			}
			log.Printf("ws error: %v (reconnect in 1s)", err)
			select {
			case <-ctx.Done():
				printSummary(start)
				return
			case <-time.After(1 * time.Second):
			}
		}
	}
}

func normalizeURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// そのまま
	default:
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	return u.String(), nil
}

func runOnce(ctx context.Context, wsURL string, verbose bool) error {
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	c, _, err := websocket.Dial(dialCtx, wsURL, nil)
	cancel()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.CloseNow()

	// 大きめのフレーム上限（payload は 100B だが余裕を持たせる）
	c.SetReadLimit(1 << 20)

	log.Printf("ws connected")

	for {
		var msg Message
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			return fmt.Errorf("read: %w", err)
		}
		count := msgCount.Add(1)
		// 概算バイト数（JSON にエンコードした場合の長さ）。payload + 固定オーバーヘッドで近似。
		approxBytes := int64(len(msg.Payload) + len(msg.Timestamp) + 40)
		bytesCount.Add(approxBytes)
		if verbose || count%12 == 0 {
			log.Printf("recv #%d seq=%d bytes=%d", count, msg.Seq, approxBytes)
		}
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
