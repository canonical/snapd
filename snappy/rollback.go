package snappy

import (
	"fmt"
	"sort"
)

// Rollback will roll the given pkg back to the given ver
// The version needs to be installed on disk
func Rollback(pkg, ver string) error {

	// no version specified, find the previous one
	if ver == "" {
		m := NewMetaRepository()
		installed, err := m.Installed()
		if err != nil {
			return err
		}
		snaps := FindSnapsByName(pkg, installed)
		if len(snaps) < 2 {
			return fmt.Errorf("no version to rollback to")
		}
		sort.Sort(BySnapVersion(snaps))
		// -1 is the most recent, -2 the previous one
		ver = snaps[len(snaps)-2].Version()
	}

	if err := makeSnapActiveByNameAndVersion(pkg, ver); err != nil {
		return err
	}

	return nil
}
