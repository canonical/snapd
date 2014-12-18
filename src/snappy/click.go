package snappy

type Click struct {

}

func (c *Click) Versions() (versions []Part) {
	// FIXME
	return versions
}

func (c *Click) Update(parts []Part) (err error) {
	// FIXME
	return err
}

func (c *Click) Rollback(parts []Part) (err error) {
	// FIXME
	return err
}

func (c *Click) Tags(part Part) (tags []string) {
	return tags
}

func (c *Click) Less(a, b Part) bool {
	// FIXME
	return false
}

func (c *Click) Privileged() bool {
	return false
}
