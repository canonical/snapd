package apparmor_test

import (
	"github.com/snapcore/cerberus/apparmor"

	. "gopkg.in/check.v1"
)

type nTypeSuite struct{}

var _ = Suite(&nTypeSuite{})

func (*nTypeSuite) TestNotificationTypeValues(c *C) {
	c.Check(apparmor.Response, Equals, apparmor.NotificationType(0))
	c.Check(apparmor.Cancel, Equals, apparmor.NotificationType(1))
	c.Check(apparmor.Interrupt, Equals, apparmor.NotificationType(2))
	c.Check(apparmor.Alive, Equals, apparmor.NotificationType(3))
	c.Check(apparmor.Operation, Equals, apparmor.NotificationType(4))
}

func (*nTypeSuite) TestNotificationTypeIsValid(c *C) {
	c.Check(apparmor.Response.IsValid(), Equals, true)
	c.Check(apparmor.Cancel.IsValid(), Equals, true)
	c.Check(apparmor.Interrupt.IsValid(), Equals, true)
	c.Check(apparmor.Alive.IsValid(), Equals, true)
	c.Check(apparmor.Operation.IsValid(), Equals, true)
	c.Check(apparmor.NotificationType(5).IsValid(), Equals, false)
}

func (*nTypeSuite) TestNotificationTypeString(c *C) {
	c.Check(apparmor.Response.String(), Equals, "response")
	c.Check(apparmor.Cancel.String(), Equals, "cancel")
	c.Check(apparmor.Interrupt.String(), Equals, "interrupt")
	c.Check(apparmor.Alive.String(), Equals, "alive")
	c.Check(apparmor.Operation.String(), Equals, "operation")
	c.Check(apparmor.NotificationType(5).String(), Equals, "NotificationType(5)")
}
