package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Tests of `recording write` keeping a turn's delay (T054k; research D34, US2.10), written before
// the change. `write` keeps the response's `fixedDelayMilliseconds` when it is an integer from 0
// to 60000 (WireMock holds the answer that long), and REFUSES a snapshot whose
// `fixedDelayMilliseconds` is out of that range or not an integer: write returns an error and
// writes neither the mapping nor the body. Every other response field is still dropped except
// status, the Content-Type header and bodyFileName. An absent delay stays absent. No Docker.

// delaySnapshot is one exchange whose response has delay as its fixedDelayMilliseconds (raw JSON,
// "" for none) and the WireMock response fields `write` must drop.
func delaySnapshot(delay string) string {
	d := ""
	if delay != "" {
		d = `"fixedDelayMilliseconds":` + delay + `,`
	}
	return `{"mappings":[{"request":{"url":"/v1/messages?beta=true","method":"POST"},
 "response":{"status":200,` + d + `
  "headers":{"Content-Type":"text/event-stream","Request-Id":"req_1"},
  "body":"event: message_stop\ndata: {}\n\n",
  "statusMessage":"OK","delayDistribution":{"type":"uniform","lower":1,"upper":2},
  "chunkedDribbleDelay":{"numberOfChunks":2,"totalDuration":5},"fault":"EMPTY_RESPONSE",
  "transformers":["response-template"],"transformerParameters":{"a":1},
  "additionalProxyRequestHeaders":{"X":"y"},"base64Body":"eA=="}}]}`
}

// writtenResponse runs write on snap and returns the mapping's response, or the error.
func writtenResponse(t *testing.T, snap string) (map[string]any, error) {
	t.Helper()
	dir := tmp(t)
	var err error
	withStdin(t, snap, func() { err = write([]string{"-dir", dir, "-name", "d"}) })
	if err != nil {
		for _, f := range []string{"mappings/d.json", "__files/d.sse"} {
			if _, serr := os.Stat(filepath.Join(dir, f)); serr == nil {
				t.Errorf("write failed but wrote %s", f)
			}
		}
		return nil, err
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, "mappings", "d.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	var m struct{ Response map[string]any }
	if jerr := json.Unmarshal(raw, &m); jerr != nil {
		t.Fatal(jerr)
	}
	return m.Response, nil
}

func keys(m map[string]any) []string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}

func TestWriteKeepsFixedDelayMillisecondsInRange(t *testing.T) {
	for _, c := range []struct {
		delay string
		want  float64
	}{{"0", 0}, {"1", 1}, {"30000", 30000}, {"60000", 60000}} {
		resp, err := writtenResponse(t, delaySnapshot(c.delay))
		if err != nil {
			t.Errorf("delay %s: write refused it: %v", c.delay, err)
			continue
		}
		if d, ok := resp["fixedDelayMilliseconds"]; !ok || d != c.want {
			t.Errorf("delay %s: fixedDelayMilliseconds = %v (present %v), want %v", c.delay, d, ok, c.want)
		}
		want := []string{"bodyFileName", "fixedDelayMilliseconds", "headers", "status"}
		if got := keys(resp); !reflect.DeepEqual(got, want) {
			t.Errorf("delay %s: response fields %v, want only %v", c.delay, got, want)
		}
		if h, _ := resp["headers"].(map[string]any); !reflect.DeepEqual(h, map[string]any{"Content-Type": "text/event-stream"}) {
			t.Errorf("delay %s: headers %v, want only Content-Type", c.delay, h)
		}
	}
}

func TestWriteWithoutDelayStillDropsEveryOtherField(t *testing.T) {
	resp, err := writtenResponse(t, delaySnapshot(""))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bodyFileName", "headers", "status"}; !reflect.DeepEqual(keys(resp), want) {
		t.Errorf("response fields %v, want only %v (no delay added)", keys(resp), want)
	}
}

func TestWriteRefusesAFixedDelayOutOfRange(t *testing.T) {
	for _, d := range []string{"-1", "60001", "1.5", `"100"`, "true"} {
		if _, err := writtenResponse(t, delaySnapshot(d)); err == nil {
			t.Errorf("fixedDelayMilliseconds %s: write accepted it, want an error", d)
		}
	}
}
