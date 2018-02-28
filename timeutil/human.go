// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package timeutil

import (
	"fmt"
	"math"
	"time"

	"github.com/snapcore/snapd/i18n"
)

func noon(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 12, 0, 0, 0, t.Location())
}

// Human turns the time (which must be in the past) into something
// more meant for human consumption.
func Human(then time.Time) string {
	return humanTimeSince(then, time.Now())
}

func humanTimeSince(then, now time.Time) string {
	then = then.Local()
	now = now.Local()

	switch d := int(math.Ceil(noon(now).Sub(noon(then)).Hours() / 24)); d {
	case 0:
		return then.Format(i18n.G("today at 15:04-07"))
	case 1:
		return then.Format(i18n.G("yesterday at 15:04-07"))
	default:
		return fmt.Sprintf(then.Format(i18n.G("%d days ago at 15:04-07")), d)
	}
}
