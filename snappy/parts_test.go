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
