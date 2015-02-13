package snappy

// ListInstalled returns all installed snaps
func ListInstalled() ([]Part, error) {
	m := NewMetaRepository()

	return m.Installed()
}

// ListUpdates returns all snaps with updates
func ListUpdates() ([]Part, error) {
	m := NewMetaRepository()

	return m.Updates()
}
