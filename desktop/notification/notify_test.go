// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package notification_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/notification"
)

func Test(t *testing.T) { TestingT(t) }

type notifySuite struct{}

var _ = Suite(&notifySuite{})

func (s *notifySuite) TestCloseReasonString(c *C) {
	c.Check(notification.CloseReasonExpired.String(), Equals, "expired")
	c.Check(notification.CloseReasonDismissed.String(), Equals, "dismissed")
	c.Check(notification.CloseReasonClosed.String(), Equals, "closed")
	c.Check(notification.CloseReasonUndefined.String(), Equals, "undefined")
	c.Check(notification.CloseReason(42).String(), Equals, "CloseReason(42)")
}
