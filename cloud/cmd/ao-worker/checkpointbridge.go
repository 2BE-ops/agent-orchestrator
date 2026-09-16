package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

// runCheckpointBridge serves a local unix socket that the harness Stop hook pokes
// on every turn completion (see runHook in ao-cloud-agent). Each poke runs one
// durable-restore checkpoint. The trigger is event-driven off turn completion,
// not a timer: a single-slot trigger channel coalesces bursts so back-to-back
// turns collapse into one in-flight checkpoint, and the checkpoint itself is
// change-detected so an unchanged poke is a no-op. It blocks until ctx is done.
func runCheckpointBridge(
	ctx context.Context,
	socketPath string,
	run func(context.Context),
	logger *slog.Logger,
) error {
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on checkpoint bridge socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("secure checkpoint bridge socket: %w", err)
	}

	// One consumer runs checkpoints serially, so overlapping Stop hooks never race
	// a git preserve or a control-plane push.
	trigger := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-trigger:
				run(ctx)
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /checkpoint", func(w http.ResponseWriter, _ *http.Request) {
		// Non-blocking, coalescing send: if a checkpoint is already queued this
		// poke is dropped. The handler returns immediately and the capture runs
		// asynchronously, so the Stop hook's short timeout is never spent waiting
		// on git or the network.
		select {
		case trigger <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	})
	server := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-errCh
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
