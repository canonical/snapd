package notify_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	. "gopkg.in/check.v1"
)

type modeSetSuite struct{}

var _ = Suite(&modeSetSuite{})

func (*modeSetSuite) TestModeSetValues(c *C) {
	c.Check(notify.APPARMOR_MODESET_AUDIT, Equals, notify.ModeSet(1))
	c.Check(notify.APPARMOR_MODESET_ALLOWED, Equals, notify.ModeSet(2))
	c.Check(notify.APPARMOR_MODESET_ENFORCE, Equals, notify.ModeSet(4))
	c.Check(notify.APPARMOR_MODESET_HINT, Equals, notify.ModeSet(8))
	c.Check(notify.APPARMOR_MODESET_STATUS, Equals, notify.ModeSet(16))
	c.Check(notify.APPARMOR_MODESET_ERROR, Equals, notify.ModeSet(32))
	c.Check(notify.APPARMOR_MODESET_KILL, Equals, notify.ModeSet(64))
	c.Check(notify.APPARMOR_MODESET_USER, Equals, notify.ModeSet(128))
}

func (*modeSetSuite) TestModeSetIsValid(c *C) {
	c.Check(notify.APPARMOR_MODESET_AUDIT.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_MODESET_ALLOWED.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_MODESET_ENFORCE.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_MODESET_HINT.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_MODESET_STATUS.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_MODESET_ERROR.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_MODESET_KILL.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_MODESET_USER.IsValid(), Equals, true)
	c.Check(notify.ModeSet(256).IsValid(), Equals, false)
}

func (*modeSetSuite) TestModeSetString(c *C) {
	var m notify.ModeSet
	c.Check(m.String(), Equals, "none")
	m |= notify.APPARMOR_MODESET_AUDIT
	c.Check(m.String(), Equals, "audit")
	m |= notify.APPARMOR_MODESET_ALLOWED
	c.Check(m.String(), Equals, "audit|allowed")
	m |= notify.APPARMOR_MODESET_ENFORCE
	c.Check(m.String(), Equals, "audit|allowed|enforce")
	m |= notify.APPARMOR_MODESET_HINT
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint")
	m |= notify.APPARMOR_MODESET_STATUS
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status")
	m |= notify.APPARMOR_MODESET_ERROR
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error")
	m |= notify.APPARMOR_MODESET_KILL
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill")
	m |= notify.APPARMOR_MODESET_USER
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user")
	c.Check(m.IsValid(), Equals, true)
	m |= 256
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user|0x100")
	c.Check(m.IsValid(), Equals, false)
}
