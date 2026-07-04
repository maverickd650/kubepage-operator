package main

import (
	"net"
	"testing"
	"time"
)

func TestWaitForAddrSucceedsOnceListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	if !waitForAddr(ln.Addr().String(), time.Second) {
		t.Error("waitForAddr = false, want true for an already-listening address")
	}
}

func TestWaitForAddrTimesOutWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	if waitForAddr(addr, 200*time.Millisecond) {
		t.Error("waitForAddr = true, want false: nothing is listening on this address")
	}
}
