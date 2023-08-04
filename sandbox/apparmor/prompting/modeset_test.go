package prompting_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/prompting"

	. "gopkg.in/check.v1"
)

type modeSetSuite struct{}

var _ = Suite(&modeSetSuite{})

func (*modeSetSuite) TestModeSetValues(c *C) {
	c.Check(prompting.APPARMOR_MODESET_AUDIT, Equals, prompting.ModeSet(1))
	c.Check(prompting.APPARMOR_MODESET_ALLOWED, Equals, prompting.ModeSet(2))
	c.Check(prompting.APPARMOR_MODESET_ENFORCE, Equals, prompting.ModeSet(4))
	c.Check(prompting.APPARMOR_MODESET_HINT, Equals, prompting.ModeSet(8))
	c.Check(prompting.APPARMOR_MODESET_STATUS, Equals, prompting.ModeSet(16))
	c.Check(prompting.APPARMOR_MODESET_ERROR, Equals, prompting.ModeSet(32))
	c.Check(prompting.APPARMOR_MODESET_KILL, Equals, prompting.ModeSet(64))
	c.Check(prompting.APPARMOR_MODESET_USER, Equals, prompting.ModeSet(128))
}

func (*modeSetSuite) TestModeSetIsValid(c *C) {
	c.Check(prompting.APPARMOR_MODESET_AUDIT.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_MODESET_ALLOWED.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_MODESET_ENFORCE.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_MODESET_HINT.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_MODESET_STATUS.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_MODESET_ERROR.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_MODESET_KILL.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_MODESET_USER.IsValid(), Equals, true)
	c.Check(prompting.ModeSet(256).IsValid(), Equals, false)
}

func (*modeSetSuite) TestModeSetString(c *C) {
	var m prompting.ModeSet
	c.Check(m.String(), Equals, "none")
	m |= prompting.APPARMOR_MODESET_AUDIT
	c.Check(m.String(), Equals, "audit")
	m |= prompting.APPARMOR_MODESET_ALLOWED
	c.Check(m.String(), Equals, "audit|allowed")
	m |= prompting.APPARMOR_MODESET_ENFORCE
	c.Check(m.String(), Equals, "audit|allowed|enforce")
	m |= prompting.APPARMOR_MODESET_HINT
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint")
	m |= prompting.APPARMOR_MODESET_STATUS
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status")
	m |= prompting.APPARMOR_MODESET_ERROR
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error")
	m |= prompting.APPARMOR_MODESET_KILL
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill")
	m |= prompting.APPARMOR_MODESET_USER
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user")
	c.Check(m.IsValid(), Equals, true)
	m |= 256
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user|0x100")
	c.Check(m.IsValid(), Equals, false)
}
