package prompting_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/prompting"

	. "gopkg.in/check.v1"
)

type mclsSuite struct{}

var _ = Suite(&mclsSuite{})

func (*mclsSuite) TestMediationClassValues(c *C) {
	// The specific values must match sys/apparmor.h
	c.Check(prompting.AA_CLASS_FILE, Equals, prompting.MediationClass(2))
	c.Check(prompting.AA_CLASS_DBUS, Equals, prompting.MediationClass(32))
}

func (*mclsSuite) TestString(c *C) {
	c.Check(prompting.AA_CLASS_FILE.String(), Equals, "AA_CLASS_FILE")
	c.Check(prompting.AA_CLASS_DBUS.String(), Equals, "AA_CLASS_DBUS")
	c.Check(prompting.MediationClass(1).String(), Equals, "MediationClass(0x1)")
}
