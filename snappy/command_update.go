package snappy

import (
    "os"
    "errors"
)

func cmdUpdate(args []string) (err error) {

	// FIXME: find a way to call this prior to executing *any* of
	// the commands (not just this "update" and "versions").
	root := os.Getuid() == 0

	parts := []DataSource{new(Click), new(SystemImage)}

	for _, part := range parts {
		if part.Privileged() == true && root != true {
			return errors.New("must be root")
		}
	}

    // FIXME: testing
    return parts[1].Update(nil)
}
