// Command engine serves the engine API on the platform's internal network.
//
// It reads its settings from the environment only: ENGINE_LISTEN_ADDRESS (such as ":8080")
// and the session's three tokens, ENGINE_CLIENT_TOKEN, ENGINE_PAGE_TOKEN and
// ENGINE_WORKER_TOKEN, which must all be set and differ from each other.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentic-platform/engine/api"
)

func main() {
	if err := run(); err != nil {
		log.Fatal("engine: ", err)
	}
}

func run() error {
	env := map[string]string{}
	for _, name := range []string{"ENGINE_LISTEN_ADDRESS", "ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN"} {
		if env[name] = os.Getenv(name); env[name] == "" {
			return fmt.Errorf("%s is not set", name)
		}
	}
	tokens := api.Tokens{Client: env["ENGINE_CLIENT_TOKEN"], Page: env["ENGINE_PAGE_TOKEN"], Worker: env["ENGINE_WORKER_TOKEN"]}
	if tokens.Client == tokens.Page || tokens.Client == tokens.Worker || tokens.Page == tokens.Worker {
		return errors.New("the client, page and worker tokens must differ")
	}

	// On SIGTERM or an interrupt, running operations are cancelled and the server stops
	// after answering the requests it is serving.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{
		Addr:              env["ENGINE_LISTEN_ADDRESS"],
		Handler:           api.NewHandler(api.Config{Tokens: tokens, Context: ctx}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
