package notify_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	. "gopkg.in/check.v1"
)

type mclsSuite struct{}

var _ = Suite(&mclsSuite{})

func (*mclsSuite) TestMediationClassValues(c *C) {
	// The specific values must match sys/apparmor.h
	c.Check(notify.AA_CLASS_FILE, Equals, notify.MediationClass(2))
	c.Check(notify.AA_CLASS_DBUS, Equals, notify.MediationClass(32))
}

func (*mclsSuite) TestString(c *C) {
	c.Check(notify.AA_CLASS_FILE.String(), Equals, "AA_CLASS_FILE")
	c.Check(notify.AA_CLASS_DBUS.String(), Equals, "AA_CLASS_DBUS")
	c.Check(notify.MediationClass(1).String(), Equals, "MediationClass(0x1)")
}
