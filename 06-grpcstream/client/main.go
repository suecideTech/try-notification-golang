package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/y-hashisaka/try-notification-golang/06-grpcstream/notifypb"
)

var (
	msgCount   atomic.Int64
	bytesCount atomic.Int64
)

func main() {
	serverAddr := flag.String("server", "localhost:50051", "grpc server address (host:port)")
	verbose := flag.Bool("verbose", false, "log every recv (default: log every 12 recv)")
	flag.Parse()

	log.Printf("grpc-stream client -> %s", *serverAddr)

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
		if err := runOnce(ctx, *serverAddr, *verbose); err != nil {
			if ctx.Err() != nil {
				printSummary(start)
				return
			}
			log.Printf("grpc error: %v (reconnect in 1s)", err)
			select {
			case <-ctx.Done():
				printSummary(start)
				return
			case <-time.After(1 * time.Second):
			}
		}
	}
}

func runOnce(ctx context.Context, addr string, verbose bool) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	client := notifypb.NewNotifierClient(conn)
	stream, err := client.Stream(ctx, &notifypb.StreamRequest{})
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	log.Printf("grpc connected")

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recv: %w", err)
		}
		count := msgCount.Add(1)
		// Approximate on-wire bytes: protobuf payload + ts + small overhead.
		approxBytes := int64(len(msg.GetPayload()) + len(msg.GetTs()) + 16)
		bytesCount.Add(approxBytes)
		if verbose || count%12 == 0 {
			log.Printf("recv #%d seq=%d bytes=%d", count, msg.GetSeq(), approxBytes)
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
