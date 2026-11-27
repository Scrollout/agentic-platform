package api

// T053g, engine side: a grant file whose object is followed by a closing `}` or `]` grants nothing.
// json.Decoder.More reports false before a closing delimiter, so only the check that the next token
// is the end of the file refuses it; no other test pins that check (T053d review). Judged from the
// owner's rule of 2026-11-13: a malformed grant file is no grant (401). Shared helpers are
// grants_dir_test.go's and workers_test.go's; none is changed.

import "testing"

func TestGrantsDirFileWithATrailingClosingDelimiterGrantsNothing(t *testing.T) {
	const grant = `{"slice":"S-1","step":"code","call_token":"c"}`
	for name, content := range map[string]string{
		"a trailing }":                 grant + `}`,
		"a trailing ]":                 grant + `]`,
		"a trailing } after a newline": grant + "\n}\n",
		"two trailing }":               grant + `}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			r, ws := grantsDirRig(t, dir, nil)
			writeRawGrantFile(t, dir, grantFileName(grantsTok), []byte(content))
			register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
			if _, _, ok := ws.Endpoint("S-1", "", "code"); ok {
				t.Error("a grant file with trailing data registered a worker")
			}
			// The same grant without the trailing delimiter is accepted.
			writeRawGrantFile(t, dir, grantFileName(grantsTok), []byte(grant+"\n"))
			decodeWorker(t, register(r, grantsTok, "http://svc:8080"))
		})
	}
}
