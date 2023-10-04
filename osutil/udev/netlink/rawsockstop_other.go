//go:build !arm64

// don't remove the newline between the above statement and the package statement
// or else the build constraint will be ignored and assumed to be part of the package comment!

package netlink

import "syscall"

// once we use something other than go1.10 we can move this back into
// rawsocketstop.go and remove rawsocketstop_arm64.go, see
// rawsocketstop_arm64.go for details
var stopperSelectTimeout = func() *syscall.Timeval {
	return nil
}
