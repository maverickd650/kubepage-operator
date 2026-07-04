package main

import "net"

// browserURL turns a listener address (as returned by net.Listener.Addr,
// e.g. "0.0.0.0:8080", "[::]:8080", or "127.0.0.1:53214" for a ":0" bind)
// into a URL a browser will actually navigate to. Browsers commonly refuse
// a literal 0.0.0.0/:: host even though it's a valid bind address meaning
// "every interface" — rewriting it to the loopback address is the same
// address space "every interface" already includes, so the page is always
// reachable there.
func browserURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	switch host {
	case "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
