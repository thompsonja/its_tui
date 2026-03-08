package step

import (
	"fmt"
	"net"
)

// RandomPort finds a random available TCP port on localhost by letting the OS
// assign one, then immediately releasing it for the caller to use.
// There is a small TOCTOU window between release and use, which is acceptable
// for local dev tooling.
func RandomPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("finding free port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
