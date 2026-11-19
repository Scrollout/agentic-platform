package main

// Tests for the engine's health command (T042): `engine health` tells the Compose healthcheck
// whether the engine listens yet. health dials ENGINE_LISTEN_ADDRESS's port on 127.0.0.1 (a host
// part, if given, is used as is) and needs no other setting. Hermetic: loopback only.

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// healthEnv is a getenv that knows ENGINE_LISTEN_ADDRESS only (set when ok), so a health that
// reads any other setting sees it empty.
func healthEnv(addr string, ok bool) func(string) string {
	return func(k string) string {
		if ok && k == "ENGINE_LISTEN_ADDRESS" {
			return addr
		}
		return ""
	}
}

func healthListener(t *testing.T) (net.Listener, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l, strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func TestHealthIsNilWhenTheListenPortAcceptsOnLoopback(t *testing.T) {
	l, port := healthListener(t)
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if err := health(healthEnv(":"+port, true)); err != nil {
		t.Fatalf("health with a listener on :%s: %v", port, err)
	}
	if err := health(healthEnv("127.0.0.1:"+port, true)); err != nil {
		t.Fatalf("health with a listener on 127.0.0.1:%s (host given): %v", port, err)
	}
}

func TestHealthFailsNamingTheAddressWhenNothingListens(t *testing.T) {
	l, port := healthListener(t)
	l.Close()
	start := time.Now()
	err := health(healthEnv(":"+port, true))
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("health took %v against a closed loopback port; want at most 3s", took)
	}
	if err == nil {
		t.Fatalf("health on a closed port :%s gave nil", port)
	}
	if want := "127.0.0.1:" + port; !strings.Contains(err.Error(), want) {
		t.Fatalf("health's error %q does not name the address %s", err, want)
	}
}

func TestHealthFailsNamingTheVariableWhenTheAddressIsUnsetOrMalformed(t *testing.T) {
	cases := map[string]func(string) string{
		"unset":        healthEnv("", false),
		"empty":        healthEnv("", true),
		"no port":      healthEnv("no-port-here", true),
		"too many ':'": healthEnv("a:b:c", true),
	}
	for name, getenv := range cases {
		t.Run(name, func(t *testing.T) {
			err := health(getenv)
			if err == nil {
				t.Fatal("health gave nil")
			}
			if !strings.Contains(err.Error(), "ENGINE_LISTEN_ADDRESS") {
				t.Fatalf("health's error %q does not name ENGINE_LISTEN_ADDRESS", err)
			}
		})
	}
}
