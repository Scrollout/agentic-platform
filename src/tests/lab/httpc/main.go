// Command httpc is the lab's HTTP client, run with `docker exec` in a container on the lab's
// internal network (the host cannot dial that network). Usage: httpc <method> <url> [<header>...];
// the request body is stdin. It prints the status, the Content-Type and the body, each on its own
// line group: "<status>\n<content-type>\n<body>".
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: httpc method url [Name: value]...")
		os.Exit(2)
	}
	var body io.Reader = os.Stdin
	req, err := http.NewRequest(os.Args[1], os.Args[2], body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if os.Args[1] == "GET" || os.Args[1] == "HEAD" {
		req.Body = nil
	}
	for _, h := range os.Args[3:] {
		k, v, _ := strings.Cut(h, ":")
		req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Printf("%d\n%s\n", resp.StatusCode, resp.Header.Get("Content-Type"))
	os.Stdout.Write(b)
}
