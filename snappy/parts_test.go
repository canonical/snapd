package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestInstalledSnapByType(c *C) {
	yamlPath, err := makeInstalledMockSnapFromPackageYaml(s.tempdir, `name: app1
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	yamlPath, err = makeInstalledMockSnapFromPackageYaml(s.tempdir, `name: framework1
version: 1.0
type: framework
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	parts, err := InstalledSnapsByType(SnapTypeApp)
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "app1")

	parts, err = InstalledSnapsByType(SnapTypeFramework)
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "framework1")
}

func (s *SnapTestSuite) TestMetaRepositoryDetails(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir)
	c.Assert(err, IsNil)

	m := NewMetaRepository()
	c.Assert(m, NotNil)

	parts, err := m.Details("hello-app")
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
}

func (s *SnapTestSuite) FindSnapsByNameNotAvailable(c *C) {
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	parts := FindSnapsByName("not-available", installed)
	c.Assert(len(parts), Equals, 0)
}

func (s *SnapTestSuite) FindSnapsByNameFound(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir)
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(len(installed), Equals, 1)

	parts := FindSnapsByName("hello-app", installed)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
}
