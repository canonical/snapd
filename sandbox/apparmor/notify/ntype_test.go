package notify_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	. "gopkg.in/check.v1"
)

type nTypeSuite struct{}

var _ = Suite(&nTypeSuite{})

func (*nTypeSuite) TestNotificationTypeValues(c *C) {
	c.Check(notify.APPARMOR_NOTIF_RESP, Equals, notify.NotificationType(0))
	c.Check(notify.APPARMOR_NOTIF_CANCEL, Equals, notify.NotificationType(1))
	c.Check(notify.APPARMOR_NOTIF_INTERRUPT, Equals, notify.NotificationType(2))
	c.Check(notify.APPARMOR_NOTIF_ALIVE, Equals, notify.NotificationType(3))
	c.Check(notify.APPARMOR_NOTIF_OP, Equals, notify.NotificationType(4))
}

func (*nTypeSuite) TestNotificationTypeIsValid(c *C) {
	c.Check(notify.APPARMOR_NOTIF_RESP.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_NOTIF_CANCEL.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_NOTIF_INTERRUPT.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_NOTIF_ALIVE.IsValid(), Equals, true)
	c.Check(notify.APPARMOR_NOTIF_OP.IsValid(), Equals, true)
	c.Check(notify.NotificationType(5).IsValid(), Equals, false)
}

func (*nTypeSuite) TestNotificationTypeString(c *C) {
	c.Check(notify.APPARMOR_NOTIF_RESP.String(), Equals, "APPARMOR_NOTIF_RESP")
	c.Check(notify.APPARMOR_NOTIF_CANCEL.String(), Equals, "APPARMOR_NOTIF_CANCEL")
	c.Check(notify.APPARMOR_NOTIF_INTERRUPT.String(), Equals, "APPARMOR_NOTIF_INTERRUPT")
	c.Check(notify.APPARMOR_NOTIF_ALIVE.String(), Equals, "APPARMOR_NOTIF_ALIVE")
	c.Check(notify.APPARMOR_NOTIF_OP.String(), Equals, "APPARMOR_NOTIF_OP")
	c.Check(notify.NotificationType(5).String(), Equals, "NotificationType(5)")
}
