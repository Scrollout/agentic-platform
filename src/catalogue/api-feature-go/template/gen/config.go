// The service's configuration: every value it reads from its environment when it starts.
//
//	LISTEN_ADDRESS  the address the service listens on; default ":8080"
package gen

import "os"

// defaultListenAddress is the address the service listens on when LISTEN_ADDRESS is unset or empty.
const defaultListenAddress = ":8080"

// ListenAddress is LISTEN_ADDRESS, or ":8080" when it is unset or empty.
func ListenAddress() string {
	if v := os.Getenv("LISTEN_ADDRESS"); v != "" {
		return v
	}
	return defaultListenAddress
}
