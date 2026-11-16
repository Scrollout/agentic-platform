package tests

import (
	"net/http"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

// handler is the server under test: always the app seam, the stub before the code step.
func handler() http.Handler { return gen.Handler(app.New()) }
