package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hookcheck/internal/demo"
)

func main() { os.Exit(run()) }

func run() int {
	address := flag.String("listen", "127.0.0.1:8080", "listen address")
	mode := flag.String("mode", "fixed", "fixed or broken")
	delay := flag.Duration("ack-delay", 0, "commit state, then delay webhook acknowledgement")
	flag.Parse()
	if *mode != "fixed" && *mode != "broken" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "mode must be fixed or broken")
		return 2
	}
	handler, err := demo.New(demo.Config{Broken: *mode == "broken", Secret: os.Getenv("HOOKCHECK_DEMO_SECRET"), AckDelay: *delay})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not listen on demo address")
		return 2
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: time.Minute + 5*time.Second, IdleTimeout: 30 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Printf("payment-demo mode=%s url=http://%s\n", *mode, listener.Addr())
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "demo server failed")
			return 2
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
		}
		<-done
	}
	return 0
}
