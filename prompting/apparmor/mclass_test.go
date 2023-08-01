package apparmor_test

import (
	"github.com/snapcore/snapd/prompting/apparmor"

	. "gopkg.in/check.v1"
)

type mclsSuite struct{}

var _ = Suite(&mclsSuite{})

func (*mclsSuite) TestMediationClassValues(c *C) {
	// The specific values must match sys/apparmor.h
	c.Check(apparmor.AA_CLASS_FILE, Equals, apparmor.MediationClass(2))
	c.Check(apparmor.AA_CLASS_DBUS, Equals, apparmor.MediationClass(32))
}

func (*mclsSuite) TestString(c *C) {
	c.Check(apparmor.AA_CLASS_FILE.String(), Equals, "AA_CLASS_FILE")
	c.Check(apparmor.AA_CLASS_DBUS.String(), Equals, "AA_CLASS_DBUS")
	c.Check(apparmor.MediationClass(1).String(), Equals, "MediationClass(0x1)")
}
