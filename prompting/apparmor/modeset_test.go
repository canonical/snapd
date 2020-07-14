package apparmor_test

import (
	"github.com/snapcore/cerberus/apparmor"

	. "gopkg.in/check.v1"
)

type modeSetSuite struct{}

var _ = Suite(&modeSetSuite{})

func (*modeSetSuite) TestModeSetValues(c *C) {
	c.Check(apparmor.ModeSetAudit, Equals, apparmor.ModeSet(1))
	c.Check(apparmor.ModeSetAllowed, Equals, apparmor.ModeSet(2))
	c.Check(apparmor.ModeSetEnforce, Equals, apparmor.ModeSet(4))
	c.Check(apparmor.ModeSetHint, Equals, apparmor.ModeSet(8))
	c.Check(apparmor.ModeSetStatus, Equals, apparmor.ModeSet(16))
	c.Check(apparmor.ModeSetError, Equals, apparmor.ModeSet(32))
	c.Check(apparmor.ModeSetKill, Equals, apparmor.ModeSet(64))
	c.Check(apparmor.ModeSetUser, Equals, apparmor.ModeSet(128))
}

func (*modeSetSuite) TestModeSetIsValid(c *C) {
	c.Check(apparmor.ModeSetAudit.IsValid(), Equals, true)
	c.Check(apparmor.ModeSetAllowed.IsValid(), Equals, true)
	c.Check(apparmor.ModeSetEnforce.IsValid(), Equals, true)
	c.Check(apparmor.ModeSetHint.IsValid(), Equals, true)
	c.Check(apparmor.ModeSetStatus.IsValid(), Equals, true)
	c.Check(apparmor.ModeSetError.IsValid(), Equals, true)
	c.Check(apparmor.ModeSetKill.IsValid(), Equals, true)
	c.Check(apparmor.ModeSetUser.IsValid(), Equals, true)
	c.Check(apparmor.ModeSet(256).IsValid(), Equals, false)
}

func (*modeSetSuite) TestModeSetString(c *C) {
	var m apparmor.ModeSet
	c.Check(m.String(), Equals, "")
	m |= apparmor.ModeSetAudit
	c.Check(m.String(), Equals, "audit")
	m |= apparmor.ModeSetAllowed
	c.Check(m.String(), Equals, "audit|allowed")
	m |= apparmor.ModeSetEnforce
	c.Check(m.String(), Equals, "audit|allowed|enforce")
	m |= apparmor.ModeSetHint
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint")
	m |= apparmor.ModeSetStatus
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status")
	m |= apparmor.ModeSetError
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error")
	m |= apparmor.ModeSetKill
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill")
	m |= apparmor.ModeSetUser
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user")
	m |= 256
	c.Check(m.String(), Equals, "audit|allowed|enforce|hint|status|error|kill|user|0x100")
}
