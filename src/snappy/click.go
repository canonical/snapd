package snappy

type Click struct {

}

func (s *Click) Versions() (versions []Part) {
	// FIXME
	return versions
}

func (s *Click) Update(parts []Part) (err error) {
	// FIXME
	return err
}

func (s *Click) Rollback(parts []Part) (err error) {
	// FIXME
	return err
}

func (s *Click) Tags(part Part) (tags []string) {
	return tags
}

func (s *Click) Less(a, b Part) bool {
	// FIXME
	return false
}
