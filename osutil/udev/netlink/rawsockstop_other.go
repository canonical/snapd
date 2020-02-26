// + build !arm64
package netlink

// once we use something other than go1.10 we can move this back into
// rawsocketstop.go and remove rawsocketstop_arm64.go, see
// rawsocketstop_arm64.go for details
var stopperSelectTimeout *syscall.Timeval
