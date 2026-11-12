// Command client is the engine's analyst client as a program, run by the analystsvc test in a
// container on the internal network: client <base-url> <token> <digest> <description>. It waits
// for the service, calls analyst.NewHTTP(...).Propose once it is up, and prints the proposal as JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/agentic-platform/engine/analyst"
	"github.com/agentic-platform/engine/slices"
)

func main() {
	base, tok, digest, desc := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	for i := 0; i < 90; i++ { // the service needs a moment to listen
		resp, err := http.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p, err := analyst.NewHTTP(base, tok, digest).Propose(ctx, slices.AnalystInput{Description: desc})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Propose:", err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(p)
}
