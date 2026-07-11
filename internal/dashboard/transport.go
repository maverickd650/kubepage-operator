package dashboard

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// awsIPv6MetadataBlock is fd00:ec2::/32, the unique-local block AWS serves
// its IPv6 instance-metadata endpoint from (fd00:ec2::254). Unlike
// 169.254.169.254 (link-local, caught by isLinkLocal below), this range is
// ULA (fc00::/7) rather than link-local (fe80::/10), so it needs its own
// explicit deny — see https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html#instancedata-inside-access.
var awsIPv6MetadataBlock = mustParseCIDR("fd00:ec2::/32")

func mustParseCIDR(s string) *net.IPNet {
	_, block, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return block
}

// isLinkLocal reports whether ip is in the link-local unicast/multicast
// ranges (169.254.0.0/16, fe80::/10, and their multicast equivalents) —
// the block cloud providers (AWS, GCP, Azure, DigitalOcean, ...) serve their
// instance-metadata endpoint from (169.254.169.254).
func isLinkLocal(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// isBlockedMetadataAddress reports whether ip is a cloud instance-metadata
// address this package unconditionally refuses to dial: every link-local
// address (isLinkLocal), plus AWS's IPv6 metadata block (awsIPv6MetadataBlock),
// which is unique-local rather than link-local and so needs its own check.
func isBlockedMetadataAddress(ip net.IP) bool {
	return isLinkLocal(ip) || awsIPv6MetadataBlock.Contains(ip)
}

// guardedDialControl is a net.Dialer.Control func that rejects a connection
// to a blocked metadata destination. It runs after DNS resolution on the
// literal IP the dialer is about to connect to, so a hostname that resolves
// to a blocked address (including via DNS rebinding) is caught the same as a
// literal one.
//
// ServiceCard ping/siteMonitor/widget URLs are deliberately trusted to
// reach cluster-internal addresses (ClusterIP/pod-CIDR ranges, typically
// RFC1918) — that's the whole point of those fields — so this does not
// attempt broad SSRF prevention. It only closes the gaps with no legitimate
// use for this feature: a widget URL pointed at the node's own cloud
// metadata endpoint would hand back cloud IAM credentials to whoever set the
// ServiceCard, which is a strictly worse outcome than anything reachable
// via a normal cluster address.
func guardedDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if isBlockedMetadataAddress(ip) {
		return fmt.Errorf("dashboard: connecting to metadata address %s is not allowed", ip)
	}
	return nil
}

// newGuardedTransport returns an http.Transport cloned from base (or
// http.DefaultTransport if base is nil) with guardedDialControl wired in.
// Used both for the poller's shared client and unifi.go/proxmox.go's
// per-target insecure-TLS clients, so every outbound widget/monitor request
// goes through the same link-local guard regardless of which transport
// handles it. Every current caller passes nil, but base stays a real
// parameter (rather than being dropped) since it's the general-purpose
// entry point newGuardedTransportWithCA and any future per-target transport
// builder would clone from.
func newGuardedTransport(base *http.Transport) *http.Transport { //nolint:unparam // base is a deliberate extension point; see doc comment
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
// ServiceCard/widget URLs.
func newGuardedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: newGuardedTransport(nil)}
}

// newGuardedTransportWithCA returns an http.Transport identical to
// newGuardedTransport, additionally trusting caPEM (a PEM-encoded
// certificate or bundle) alongside the system trust store. Used when a
// ServiceWidget/InfoWidget sets caCert, so a self-hosted upstream with a
// private CA can be verified instead of falling back to insecureTLS (see
// unifi.go's InsecureTLS option, the only other TLS-verification escape
// hatch in this package).
func newGuardedTransportWithCA(caPEM []byte) (*http.Transport, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if ok := pool.AppendCertsFromPEM(caPEM); !ok {
		return nil, fmt.Errorf("dashboard: no valid PEM certificates found in caCert")
	}

	transport := newGuardedTransport(nil)
	transport.TLSClientConfig = &tls.Config{RootCAs: pool}
	return transport, nil
}
