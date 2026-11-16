// Command notes serves the notes API.
package main

import (
	"log"
	"net/http"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

func main() {
	log.Fatal(http.ListenAndServe(":8080", gen.Handler(app.New())))
}
