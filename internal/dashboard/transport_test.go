package dashboard

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestIsBlockedMetadataAddress(t *testing.T) {
	tests := map[string]struct {
		ip      string
		blocked bool
	}{
		"AWS/GCP/Azure IPv4 metadata address":        {ip: "169.254.169.254", blocked: true},
		"link-local IPv4 range":                      {ip: "169.254.1.1", blocked: true},
		"link-local IPv6 range":                      {ip: "fe80::1", blocked: true},
		"AWS IPv6 metadata address":                  {ip: "fd00:ec2::254", blocked: true},
		"another address inside the AWS block":       {ip: "fd00:ec2:1234::1", blocked: true},
		"ordinary private IPv4 address":              {ip: "10.0.0.1", blocked: false},
		"ordinary public IPv4 address":               {ip: "8.8.8.8", blocked: false},
		"unique-local address outside the AWS block": {ip: "fd00:1234::1", blocked: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) = nil", tc.ip)
			}
			if got := isBlockedMetadataAddress(ip); got != tc.blocked {
				t.Errorf("isBlockedMetadataAddress(%q) = %v, want %v", tc.ip, got, tc.blocked)
			}
		})
	}
}

func TestGuardedDialControlRejectsBlockedAddresses(t *testing.T) {
	tests := map[string]struct {
		address string
		wantErr bool
	}{
		"link-local address is rejected":                                   {address: "169.254.169.254:443", wantErr: true},
		"AWS IPv6 metadata address is rejected":                            {address: "[fd00:ec2::254]:443", wantErr: true},
		"ordinary address is allowed":                                      {address: "10.0.0.1:443", wantErr: false},
		"unparseable host is allowed through (DNS name, not yet resolved)": {address: "example.com:443", wantErr: false},
		"address with no port at all is allowed through":                   {address: "no-port-here", wantErr: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := guardedDialControl("tcp", tc.address, nil)
			if (err != nil) != tc.wantErr {
				t.Errorf("guardedDialControl(%q) error = %v, wantErr %v", tc.address, err, tc.wantErr)
			}
		})
	}
}

// TestNewGuardedTransportClonesSuppliedBase covers newGuardedTransport's
// non-nil base branch — every current caller passes nil (see its doc
// comment), but base remains a real parameter for a future per-target
// transport builder, so this proves cloning-and-guarding a supplied base
// actually works rather than only ever being exercised via the nil path.
func TestNewGuardedTransportClonesSuppliedBase(t *testing.T) {
	base := &http.Transport{MaxIdleConns: 7}
	got := newGuardedTransport(base)
	if got == base {
		t.Error("newGuardedTransport(base) returned base itself, want a clone")
	}
	if got.MaxIdleConns != 7 {
		t.Errorf("newGuardedTransport(base).MaxIdleConns = %d, want 7 (cloned from base)", got.MaxIdleConns)
	}
	if got.DialContext == nil {
		t.Error("newGuardedTransport(base) did not wire the link-local dial guard")
	}
}

func TestNewGuardedTransportWithCA(t *testing.T) {
	t.Run("valid PEM certificate is accepted", func(t *testing.T) {
		pemBytes := generateTestCACertPEM(t)
		transport, err := newGuardedTransportWithCA(pemBytes)
		if err != nil {
			t.Fatalf("newGuardedTransportWithCA() error = %v", err)
		}
		if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
			t.Fatal("newGuardedTransportWithCA() did not set RootCAs")
		}
		if transport.DialContext == nil {
			t.Error("newGuardedTransportWithCA() did not wire the link-local dial guard")
		}
	})

	t.Run("invalid PEM data is rejected", func(t *testing.T) {
		_, err := newGuardedTransportWithCA([]byte("not a valid PEM certificate"))
		if err == nil {
			t.Fatal("newGuardedTransportWithCA() error = nil, want error for invalid PEM")
		}
	})
}

// generateTestCACertPEM returns a minimal self-signed CA certificate in PEM
// form, for exercising newGuardedTransportWithCA's happy path.
func generateTestCACertPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
