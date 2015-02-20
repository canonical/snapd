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

func (s *SnapTestSuite) makeTwoTestSnaps(c *C) {
	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	snapFile := s.makeTestSnap(c, packageYaml+"version: 1.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated), IsNil)

	snapFile = s.makeTestSnap(c, packageYaml+"version: 2.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated), IsNil)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(len(installed), Equals, 2)
}

func (s *SnapTestSuite) makeOemSnap(c *C) {
	packageYaml := `name: foo.oem
icon: oem.svg
vendor: Foo Bar <foo@oem.com>
type: oem
`
	snapFile := s.makeTestSnap(c, packageYaml+"version: 1.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated), IsNil)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(len(installed), Equals, 1)
}

func (s *SnapTestSuite) TestSnapRemoveByVersion(c *C) {
	s.makeTwoTestSnaps(c)

	err := Remove("foo=1.0")

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Version(), Equals, "2.0")
}

func (s *SnapTestSuite) TestSnapRemoveActive(c *C) {
	s.makeTwoTestSnaps(c)

	err := Remove("foo")

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Version(), Equals, "1.0")
}

func (s *SnapTestSuite) TestSnapRemoveOemFails(c *C) {
	s.makeOemSnap(c)

	err := Remove("foo.oem")

	c.Assert(err, DeepEquals, ErrPackageNotRemovable)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Name(), Equals, "foo.oem")
	c.Assert(installed[0].Type(), Equals, SnapTypeOem)
}
