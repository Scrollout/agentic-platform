package tests

import (
	"net/http"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

func handler() http.Handler { return gen.Handler(app.New()) }
