package snappy

import "fmt"

func Remove(partName string) error {
	part := InstalledSnapByName(partName)
	if part == nil {
		return fmt.Errorf("Can not find snap %s", partName)
	}
	if err := part.Uninstall(); err != nil {
		return err
	}

	return nil
}
