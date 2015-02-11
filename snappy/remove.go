package snappy

import (
	"fmt"
	"strings"
)

// Remove a part by a partSpec string, this can be "name" or "name=version"
func Remove(partSpec string) error {
	var part Part
	// Note that "=" is not legal in a snap name
	if strings.Count(partSpec, "=") == 1 {
		name := strings.Split(partSpec, "=")[0]
		ver := strings.Split(partSpec, "=")[1]
		installed, err := NewMetaRepository().Installed()
		if err != nil {
			return err
		}
		part = FindSnapByNameAndVersion(name, ver, installed)
	} else {
		part = ActiveSnapByName(partSpec)
	}

	if part == nil {
		return fmt.Errorf("Can not find snap %s", partSpec)
	}
	if err := part.Uninstall(); err != nil {
		return err
	}

	return nil
}
