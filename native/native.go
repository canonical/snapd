package native

/*
#include <unistd.h>
*/
import "C"

// Isatty is a wrapper around isatty(3).
// Returns true if the specified fd is associated with a tty.
func Isatty(fd int) bool {
	return C.isatty(C.int(fd)) == 1
}
