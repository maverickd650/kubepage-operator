package main

import (
	"net"
	"os/exec"
	"runtime"
	"time"
)

// openBrowser best-effort launches the OS default browser at url. A failure
// here isn't fatal to preview mode — the dashboard is still reachable by
// navigating to url manually.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// waitForAddr polls addr until something accepts a TCP connection or
// timeout elapses, so runPreview's --open doesn't race the HTTP server's
// own ListenAndServe.
func waitForAddr(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
