package main

import "testing"

const wantLoopbackURL = "http://127.0.0.1:8080"

func TestBrowserURL(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"127.0.0.1:8080", wantLoopbackURL},
		{"0.0.0.0:8080", wantLoopbackURL},
		{"[::]:8080", wantLoopbackURL},
		{"192.168.1.5:53214", "http://192.168.1.5:53214"},
		{"not-a-valid-addr", "http://not-a-valid-addr"},
	}
	for _, tt := range tests {
		if got := browserURL(tt.addr); got != tt.want {
			t.Errorf("browserURL(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}
