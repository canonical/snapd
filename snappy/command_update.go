package snappy

import (
	"errors"
)

func cmdUpdate(args []string) (err error) {

	if len(args) < 1 {
		return errors.New("missing part")
	}

	// FIXME: validate supplied part!

	parts := []Repository{NewSystemImageRepository()}

	// FIXME: testing
	updates, err := parts[0].GetUpdates()
	return updates[0].Install()
}
