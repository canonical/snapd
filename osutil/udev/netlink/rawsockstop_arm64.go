package netlink

import (
	"math"
	"syscall"
)

// workaround a bug in go1.10 where syscall.Select() with nil Timeval
// panics (c.f. https://github.com/golang/go/issues/24189)
var stopperSelectTimeout = func() *syscall.Timeval {
	return &syscall.Timeval{
		Sec: math.MaxInt64,
	}
}
