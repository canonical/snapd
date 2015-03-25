package snappy

import (
	"strings"
)

// Remove a part by a partSpec string, this can be "name" or "name=version"
func Remove(partSpec string) error {
	var part Part
	// Note that "=" is not legal in a snap name or a snap version
	l := strings.Split(partSpec, "=")
	if len(l) == 2 {
		name := l[0]
		version := l[1]
		installed, err := NewMetaRepository().Installed()
		if err != nil {
			return err
		}
		part = FindSnapByNameAndVersion(name, version, installed)
	} else {
		part = ActiveSnapByName(partSpec)
	}

	if part == nil {
		return ErrPackageNotFound
	}

	return part.Uninstall()
}
