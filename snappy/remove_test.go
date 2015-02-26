package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestRemoveNonExistingRaisesError(c *C) {
	pkgName := "some-random-non-existing-stuff"
	err := Remove(pkgName)
	c.Assert(err, NotNil)
	c.Assert(err, Equals, ErrPackageNotFound)
}

func (s *SnapTestSuite) TestSnapRemoveByVersion(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)

	err := Remove("foo=1.0")

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Version(), Equals, "2.0")
}

func (s *SnapTestSuite) TestSnapRemoveActive(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)

	err := Remove("foo")

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Version(), Equals, "1.0")
}

func (s *SnapTestSuite) TestSnapRemoveActiveOemFails(c *C) {
	makeTwoTestSnaps(c, SnapTypeOem)

	err := Remove("foo")
	c.Assert(err, DeepEquals, ErrPackageNotRemovable)

	err = Remove("foo=1.0")
	c.Assert(err, IsNil)

	err = Remove("foo")
	c.Assert(err, DeepEquals, ErrPackageNotRemovable)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Name(), Equals, "foo")
	c.Assert(installed[0].Type(), Equals, SnapTypeOem)
	c.Assert(installed[0].Version(), Equals, "2.0")
	c.Assert(len(installed), Equals, 1)
}
