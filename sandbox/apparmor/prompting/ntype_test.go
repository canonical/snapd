package prompting_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/prompting"

	. "gopkg.in/check.v1"
)

type nTypeSuite struct{}

var _ = Suite(&nTypeSuite{})

func (*nTypeSuite) TestNotificationTypeValues(c *C) {
	c.Check(prompting.APPARMOR_NOTIF_RESP, Equals, prompting.NotificationType(0))
	c.Check(prompting.APPARMOR_NOTIF_CANCEL, Equals, prompting.NotificationType(1))
	c.Check(prompting.APPARMOR_NOTIF_INTERRUPT, Equals, prompting.NotificationType(2))
	c.Check(prompting.APPARMOR_NOTIF_ALIVE, Equals, prompting.NotificationType(3))
	c.Check(prompting.APPARMOR_NOTIF_OP, Equals, prompting.NotificationType(4))
}

func (*nTypeSuite) TestNotificationTypeIsValid(c *C) {
	c.Check(prompting.APPARMOR_NOTIF_RESP.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_NOTIF_CANCEL.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_NOTIF_INTERRUPT.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_NOTIF_ALIVE.IsValid(), Equals, true)
	c.Check(prompting.APPARMOR_NOTIF_OP.IsValid(), Equals, true)
	c.Check(prompting.NotificationType(5).IsValid(), Equals, false)
}

func (*nTypeSuite) TestNotificationTypeString(c *C) {
	c.Check(prompting.APPARMOR_NOTIF_RESP.String(), Equals, "APPARMOR_NOTIF_RESP")
	c.Check(prompting.APPARMOR_NOTIF_CANCEL.String(), Equals, "APPARMOR_NOTIF_CANCEL")
	c.Check(prompting.APPARMOR_NOTIF_INTERRUPT.String(), Equals, "APPARMOR_NOTIF_INTERRUPT")
	c.Check(prompting.APPARMOR_NOTIF_ALIVE.String(), Equals, "APPARMOR_NOTIF_ALIVE")
	c.Check(prompting.APPARMOR_NOTIF_OP.String(), Equals, "APPARMOR_NOTIF_OP")
	c.Check(prompting.NotificationType(5).String(), Equals, "NotificationType(5)")
}
