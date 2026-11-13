//go:build ignore

package main

import "os"

func main() {
	if err := os.WriteFile("gen.go", []byte("package gen\n\nconst Version = 1\n"), 0o644); err != nil {
		panic(err)
	}
}
