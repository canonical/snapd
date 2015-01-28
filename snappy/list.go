package snappy

func ListInstalled() ([]Part, error) {
	m := NewMetaRepository()

	return m.Installed()
}

func ListUpdates() ([]Part, error) {
	m := NewMetaRepository()

	return m.Updates()
}
