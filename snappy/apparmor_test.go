package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestSnappyGetAaProfile(c *C) {
	m := packageYaml{Name: "foo",
		Version: "1.0"}

	b := Binary{Name: "bin/app"}
	c.Assert(getAaProfile(&m, b.Name, &b.SecurityDefinitions), Equals, "foo_app_1.0")

	b = Binary{
		Name: "bin/app",
		SecurityDefinitions: SecurityDefinitions{
			SecurityTemplate: "some-security-json",
		},
	}
	c.Assert(getAaProfile(&m, b.Name, &b.SecurityDefinitions), Equals, "some-security-json")

	b = Binary{
		Name: "bin/app",
		SecurityDefinitions: SecurityDefinitions{
			SecurityPolicy: "some-profile",
		},
	}
	c.Assert(getAaProfile(&m, b.Name, &b.SecurityDefinitions), Equals, "some-profile")
}
