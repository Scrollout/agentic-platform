// Command notes serves the notes API.
package main

import (
	"log"
	"net/http"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

func main() {
	log.Fatal(http.ListenAndServe(gen.ListenAddress(), gen.Handler(app.New())))
}
