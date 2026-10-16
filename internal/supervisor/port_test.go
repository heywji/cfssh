package supervisor

import (
	"net"
	"testing"
)

func TestPickPortSkipsBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port
	got, err := PickPort(busy)
	if err != nil {
		t.Fatal(err)
	}
	if got == busy {
		t.Fatalf("picked busy port %d", got)
	}
}
