// Command engine serves the engine API on the platform's internal network.
//
// It reads its settings from the environment only, all required: ENGINE_LISTEN_ADDRESS (such
// as ":8080"); the session's three tokens ENGINE_CLIENT_TOKEN, ENGINE_PAGE_TOKEN and
// ENGINE_WORKER_TOKEN; AP_LINT_URL and AP_LINT_TOKEN for the contract-lint service;
// AP_ANALYST_URL and AP_ANALYST_TOKEN for the analyst service; and ENGINE_STORE_DIR, the existing
// directory of the run store. The five tokens must all differ. See setup.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal("engine: ", err)
	}
}

func run() error {
	handler, closeFn, err := setup(os.Getenv)
	if err != nil {
		return err
	}
	defer closeFn()

	// On SIGTERM or an interrupt, the server stops after answering the requests it is serving;
	// then running operations are cancelled and the store is closed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{
		Addr:              os.Getenv("ENGINE_LISTEN_ADDRESS"),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// ListenAndServe returns as soon as Shutdown starts; wait until the requests being served
	// are answered before the deferred close cancels operations and closes the store.
	<-drained
	return nil
}
