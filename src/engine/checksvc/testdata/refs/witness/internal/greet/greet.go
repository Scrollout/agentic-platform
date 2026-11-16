package greet

import "strings"

// Banner is set by a package-level initialiser, so package initialisation runs banner.
var Banner = banner()

var ready bool

func init() {
	ready = true
}

func banner() string {
	return strings.ToUpper("hello")
}

// Greet greets name. The planted bug: it leaves the name out.
func Greet(name string) string {
	if name == "" {
		return "Hello!"
	}
	return "Hello!"
}
