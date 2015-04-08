package snappy

import (
	"os"

	"launchpad.net/snappy/native"
)

// AttachedToTerminal returns true if the calling process is attached to
// a terminal device.
func AttachedToTerminal() bool {
	fd := int(os.Stdin.Fd())

	return native.Isatty(fd)
}
