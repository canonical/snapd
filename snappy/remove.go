package snappy

func Remove(partName string) error {
	part := GetInstalledSnappByName(partName)
	if part != nil {
		if err := part.Uninstall(); err != nil {
			return err
		}
	}

	return nil
}
