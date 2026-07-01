package dashboard

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// isLinkLocal reports whether ip is in the link-local unicast/multicast
// ranges (169.254.0.0/16, fe80::/10, and their multicast equivalents) —
// the block cloud providers (AWS, GCP, Azure, DigitalOcean, ...) serve their
// instance-metadata endpoint from (169.254.169.254, or fd00:ec2::254 on AWS
// IPv6 — that one is unique-local, not link-local, so it isn't caught here;
// see the doc comment on guardedDialControl for why that's an accepted gap).
func isLinkLocal(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// guardedDialControl is a net.Dialer.Control func that rejects a connection
// to a link-local destination. It runs after DNS resolution on the literal
// IP the dialer is about to connect to, so a hostname that resolves to a
// link-local address (including via DNS rebinding) is caught the same as a
// literal link-local URL.
//
// ServiceEntry ping/siteMonitor/widget URLs are deliberately trusted to
// reach cluster-internal addresses (ClusterIP/pod-CIDR ranges, typically
// RFC1918) — that's the whole point of those fields — so this does not
// attempt broad SSRF prevention. It only closes the one gap with no
// legitimate use for this feature: a widget URL pointed at the node's own
// link-local metadata endpoint would hand back cloud IAM credentials to
// whoever set the ServiceEntry, which is a strictly worse outcome than
// anything reachable via a normal cluster address.
func guardedDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if isLinkLocal(ip) {
		return fmt.Errorf("dashboard: connecting to link-local address %s is not allowed", ip)
	}
	return nil
}

// newGuardedTransport returns an http.Transport cloned from base (or
// http.DefaultTransport if base is nil) with guardedDialControl wired in.
// Used both for the poller's shared client and unifi.go's per-target
// insecure-TLS client, so every outbound widget/monitor request goes through
// the same link-local guard regardless of which transport handles it.
func newGuardedTransport(base *http.Transport) *http.Transport {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // http.DefaultTransport is always *http.Transport
	} else {
		base = base.Clone()
	}
	base.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   guardedDialControl,
	}).DialContext
	return base
}

// newGuardedHTTPClient returns an *http.Client with timeout and the
// link-local dial guard (see newGuardedTransport), for polling user-supplied
// ServiceEntry/widget URLs.
func newGuardedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: newGuardedTransport(nil)}
}
