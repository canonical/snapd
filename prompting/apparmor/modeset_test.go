package apparmor_test

import (
	"github.com/snapcore/snapd/prompting/apparmor"

	. "gopkg.in/check.v1"
)

type modeSetSuite struct{}

var _ = Suite(&modeSetSuite{})

func (*modeSetSuite) TestModeSetValues(c *C) {
	c.Check(apparmor.APPARMOR_MODESET_AUDIT, Equals, apparmor.ModeSet(1))
	c.Check(apparmor.APPARMOR_MODESET_ALLOWED, Equals, apparmor.ModeSet(2))
	c.Check(apparmor.APPARMOR_MODESET_ENFORCE, Equals, apparmor.ModeSet(4))
	c.Check(apparmor.APPARMOR_MODESET_HINT, Equals, apparmor.ModeSet(8))
	c.Check(apparmor.APPARMOR_MODESET_STATUS, Equals, apparmor.ModeSet(16))
	c.Check(apparmor.APPARMOR_MODESET_ERROR, Equals, apparmor.ModeSet(32))
	c.Check(apparmor.APPARMOR_MODESET_KILL, Equals, apparmor.ModeSet(64))
	c.Check(apparmor.APPARMOR_MODESET_USER, Equals, apparmor.ModeSet(128))
}

func (*modeSetSuite) TestModeSetIsValid(c *C) {
	c.Check(apparmor.APPARMOR_MODESET_AUDIT.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_MODESET_ALLOWED.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_MODESET_ENFORCE.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_MODESET_HINT.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_MODESET_STATUS.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_MODESET_ERROR.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_MODESET_KILL.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_MODESET_USER.IsValid(), Equals, true)
	c.Check(apparmor.ModeSet(256).IsValid(), Equals, false)
}

func (*modeSetSuite) TestModeSetString(c *C) {
	var m apparmor.ModeSet
	c.Check(m.String(), Equals, "none")
	m |= apparmor.APPARMOR_MODESET_AUDIT
	c.Check(m.String(), Equals, "audit")
	m |= apparmor.APPARMOR_MODESET_ALLOWED
	c.Check(m.String(), Equals, "audit|allowed")
	m |= apparmor.APPARMOR_MODESET_ENFORCE
	c.Check(m.String(), Equals, "audit|allowed|enforce")
	m |= apparmor.APPARMOR_MODESET_HINT
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint")
	m |= apparmor.APPARMOR_MODESET_STATUS
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status")
	m |= apparmor.APPARMOR_MODESET_ERROR
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error")
	m |= apparmor.APPARMOR_MODESET_KILL
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill")
	m |= apparmor.APPARMOR_MODESET_USER
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user")
	m |= 256
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user|0x100")
}
