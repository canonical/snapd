package snappy

type SystemImage struct {

}

func (s *SystemImage) Versions() (versions []Part) {
	// FIXME
	return versions
}

func (s *SystemImage) Update(parts []Part) (err error) {
	// FIXME
	return err
}

func (s *SystemImage) Rollback(parts []Part) (err error) {
	// FIXME
	return err
}

func (s *SystemImage) Tags(part Part) (tags []string) {
	return tags
}

func (s *SystemImage) Less(a, b Part) bool {
	// FIXME
	return false
}
