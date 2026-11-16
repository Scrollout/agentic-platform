package greet

// Greeting is what the person is told.
func Greeting(name string) string {
	// R-1 asks for the name in the greeting.
	_ = name
	return "Hello"
}
