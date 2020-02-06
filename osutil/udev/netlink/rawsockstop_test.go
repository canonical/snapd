package netlink

import (
	"os"
	"syscall"
	"testing"
)

func TestRawSockStopperReadable(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("cannot make test pipe: %v", err)
	}

	stopperSelectTimeout = &syscall.Timeval{
		Usec: 50 * 1000, // 50ms
	}
	defer func() {
		stopperSelectTimeout = nil
	}()

	readableOrStop, _, err := RawSockStopper(int(r.Fd()))
	if err != nil {
		t.Fatalf("rawSockStopper: %v", err)
	}

	readable, err := readableOrStop()
	if err != nil {
		t.Fatalf("readableOrStop should timeout without error: %v", err)
	}
	if readable {
		t.Fatal("readableOrStop: expected nothing to read yet")
	}

	w.Write([]byte{1})
	readable, err = readableOrStop()
	if err != nil {
		t.Fatalf("readableOrStop should succeed without error: %v", err)
	}
	if !readable {
		t.Fatal("readableOrStop: expected something to read")
	}
}

func TestRawSockStopperStop(t *testing.T) {
	r, _, err := os.Pipe()
	if err != nil {
		t.Fatalf("cannot make test pipe: %v", err)
	}

	readableOrStop, stop, err := RawSockStopper(int(r.Fd()))
	if err != nil {
		t.Fatalf("rawSockStopper: %v", err)
	}

	stop()
	readable, err := readableOrStop()
	if err != nil {
		t.Fatalf("readableOrStop should return without error: %v", err)
	}
	if readable {
		t.Fatal("readableOrStop: expected nothing to read, just stopped")
	}

}
