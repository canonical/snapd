package apparmor_test

import (
	"github.com/snapcore/cerberus/apparmor"

	. "gopkg.in/check.v1"
)

type mclsSuite struct{}

var _ = Suite(&mclsSuite{})

func (*mclsSuite) TestMediationClassValues(c *C) {
	// The specific values must match sys/apparmor.h
	c.Check(apparmor.MediationClassFile, Equals, apparmor.MediationClass(2))
	c.Check(apparmor.MediationClassDBus, Equals, apparmor.MediationClass(32))
}

func (*mclsSuite) TestString(c *C) {
	c.Check(apparmor.MediationClassFile.String(), Equals, "file")
	c.Check(apparmor.MediationClassDBus.String(), Equals, "D-Bus")
	c.Check(apparmor.MediationClass(1).String(), Equals, "MediationClass(0x1)")
}
