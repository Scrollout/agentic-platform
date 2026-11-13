// Command page serves the read-only run page (research D18).
//
// It reads its settings from the environment only, all required: PAGE_LISTEN_ADDRESS (such as
// ":8080"); PAGE_HOST, the one Host header it answers (such as "127.0.0.1:8123"); ENGINE_URL, the
// engine's base URL; and ENGINE_TOKEN, the page's own token for the engine.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentic-platform/page"
)

// variables are the settings the page reads, all required.
var variables = []string{"PAGE_LISTEN_ADDRESS", "PAGE_HOST", "ENGINE_URL", "ENGINE_TOKEN"}

func main() {
	if err := run(); err != nil {
		log.Fatal("page: ", err)
	}
}

func run() error {
	env := map[string]string{}
	for _, name := range variables {
		if env[name] = os.Getenv(name); env[name] == "" {
			return fmt.Errorf("%s is not set", name)
		}
	}
	if u, err := url.Parse(env["ENGINE_URL"]); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("ENGINE_URL is not an http(s) URL with a host")
	}
	if _, _, err := net.SplitHostPort(env["PAGE_HOST"]); err != nil {
		return errors.New("PAGE_HOST is not a host and port")
	}
	handler := page.NewHandler(page.Config{EngineURL: env["ENGINE_URL"], Token: env["ENGINE_TOKEN"], Host: env["PAGE_HOST"]})

	// On SIGTERM or an interrupt, the server stops after answering the requests it is serving.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{
		Addr:              env["PAGE_LISTEN_ADDRESS"],
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
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
	<-drained
	return nil
}
