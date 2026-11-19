package main

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

// healthTimeout is the longest a health call waits for the engine to accept the connection.
const healthTimeout = 2 * time.Second

// health reports whether the engine accepts connections on its listen address, which it does only
// once it has reached the durable service and serves (setup). It reads ENGINE_LISTEN_ADDRESS only;
// an address without a host is dialled on 127.0.0.1. It is the image's healthcheck (`/engine
// health`; the image has no shell), so `ap up`'s `compose up --wait` returns once the engine is ready.
func health(getenv func(string) string) error {
	listen := getenv("ENGINE_LISTEN_ADDRESS")
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("ENGINE_LISTEN_ADDRESS %q is not host:port: %w", listen, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("ENGINE_LISTEN_ADDRESS %q has no valid port", listen)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, port)
	c, err := net.DialTimeout("tcp", addr, healthTimeout)
	if err != nil {
		return fmt.Errorf("the engine does not accept connections on %s: %w", addr, err)
	}
	return c.Close()
}
