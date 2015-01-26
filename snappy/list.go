package snappy

func ListInstalled() ([]Part, error) {
	m := NewMetaRepository()

	return m.GetInstalled()
}

func ListUpdates() ([]Part, error) {
	m := NewMetaRepository()

	return m.GetUpdates()
}
