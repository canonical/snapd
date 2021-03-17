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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/notification"
)

type hintsSuite struct{}

var _ = Suite(&hintsSuite{})

func (s *hintsSuite) TestWithActionIcons(c *C) {
	val := true
	c.Check(notification.WithActionIcons(), DeepEquals, notification.Hint{Name: "action-icons", Value: &val})
}

func (s *hintsSuite) TestWithUrgency(c *C) {
	val := notification.LowUrgency
	c.Check(notification.WithUrgency(val), DeepEquals, notification.Hint{Name: "urgency", Value: &val})
}

func (s *hintsSuite) TestWithCategory(c *C) {
	val := notification.DeviceCategory
	c.Check(notification.WithCategory(val), DeepEquals, notification.Hint{Name: "category", Value: &val})
}

func (s *hintsSuite) TestWithDesktopEntry(c *C) {
	val := "desktop-name"
	c.Check(notification.WithDesktopEntry(val), DeepEquals, notification.Hint{Name: "desktop-entry", Value: &val})
}

func (s *hintsSuite) TestWithTransient(c *C) {
	val := true
	c.Check(notification.WithTransient(), DeepEquals, notification.Hint{Name: "transient", Value: &val})
}

func (s *hintsSuite) TestWithResident(c *C) {
	val := true
	c.Check(notification.WithResident(), DeepEquals, notification.Hint{Name: "resident", Value: &val})
}

func (s *hintsSuite) TestWithPointToX(c *C) {
	val := 10
	c.Check(notification.WithPointToX(val), DeepEquals, notification.Hint{Name: "x", Value: &val})
}

func (s *hintsSuite) TestWithPointToY(c *C) {
	val := 10
	c.Check(notification.WithPointToY(val), DeepEquals, notification.Hint{Name: "y", Value: &val})
}

func (s *hintsSuite) TestWithImageFile(c *C) {
	val := "/path/to/img"
	c.Check(notification.WithImageFile(val), DeepEquals, notification.Hint{Name: "image-path", Value: &val})
}

func (s *hintsSuite) TestWithSoundFile(c *C) {
	val := "/path/to/snd"
	c.Check(notification.WithSoundFile(val), DeepEquals, notification.Hint{Name: "sound-file", Value: &val})
}

func (s *hintsSuite) TestWithSoundName(c *C) {
	val := "sound"
	c.Check(notification.WithSoundName(val), DeepEquals, notification.Hint{Name: "sound-name", Value: &val})
}

func (s *hintsSuite) TestWithSuppressSound(c *C) {
	val := true
	c.Check(notification.WithSuppressSound(), DeepEquals, notification.Hint{Name: "suppress-sound", Value: &val})
}

type urgencySuite struct{}

var _ = Suite(&urgencySuite{})

func (s *urgencySuite) TestString(c *C) {
	c.Check(notification.LowUrgency.String(), Equals, "low")
	c.Check(notification.NormalUrgency.String(), Equals, "normal")
	c.Check(notification.CriticalUrgency.String(), Equals, "critical")
	c.Check(notification.Urgency(13).String(), Equals, "Urgency(13)")
}
