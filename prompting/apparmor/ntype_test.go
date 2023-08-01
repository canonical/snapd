package apparmor_test

import (
	"github.com/snapcore/snapd/prompting/apparmor"

	. "gopkg.in/check.v1"
)

type nTypeSuite struct{}

var _ = Suite(&nTypeSuite{})

func (*nTypeSuite) TestNotificationTypeValues(c *C) {
	c.Check(apparmor.APPARMOR_NOTIF_RESP, Equals, apparmor.NotificationType(0))
	c.Check(apparmor.APPARMOR_NOTIF_CANCEL, Equals, apparmor.NotificationType(1))
	c.Check(apparmor.APPARMOR_NOTIF_INTERRUPT, Equals, apparmor.NotificationType(2))
	c.Check(apparmor.APPARMOR_NOTIF_ALIVE, Equals, apparmor.NotificationType(3))
	c.Check(apparmor.APPARMOR_NOTIF_OP, Equals, apparmor.NotificationType(4))
}

func (*nTypeSuite) TestNotificationTypeIsValid(c *C) {
	c.Check(apparmor.APPARMOR_NOTIF_RESP.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_NOTIF_CANCEL.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_NOTIF_INTERRUPT.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_NOTIF_ALIVE.IsValid(), Equals, true)
	c.Check(apparmor.APPARMOR_NOTIF_OP.IsValid(), Equals, true)
	c.Check(apparmor.NotificationType(5).IsValid(), Equals, false)
}

func (*nTypeSuite) TestNotificationTypeString(c *C) {
	c.Check(apparmor.APPARMOR_NOTIF_RESP.String(), Equals, "APPARMOR_NOTIF_RESP")
	c.Check(apparmor.APPARMOR_NOTIF_CANCEL.String(), Equals, "APPARMOR_NOTIF_CANCEL")
	c.Check(apparmor.APPARMOR_NOTIF_INTERRUPT.String(), Equals, "APPARMOR_NOTIF_INTERRUPT")
	c.Check(apparmor.APPARMOR_NOTIF_ALIVE.String(), Equals, "APPARMOR_NOTIF_ALIVE")
	c.Check(apparmor.APPARMOR_NOTIF_OP.String(), Equals, "APPARMOR_NOTIF_OP")
	c.Check(apparmor.NotificationType(5).String(), Equals, "NotificationType(5)")
}
