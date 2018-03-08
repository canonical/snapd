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

// Human turns the time into a relative expression of time meant for human
// consumption.
// Human(t)  --> "today at 07:47"
func Human(then time.Time) string {
	return humanTimeSince(then.Local(), time.Now().Local())
}

func humanTimeSince(then, now time.Time) string {
	d := int(math.Floor(noon(then).Sub(noon(now)).Hours() / 24))
	switch {
	case d < -1:
		// TRANSLATORS: %d will be at least 2; the singular is only included to help gettext
		return fmt.Sprintf(then.Format(i18n.NG("%d day ago, at 15:04 MST", "%d days ago, at 15:04 MST", -d)), -d)
	case d == -1:
		return then.Format(i18n.G("yesterday at 15:04 MST"))
	case d == 0:
		return then.Format(i18n.G("today at 15:04 MST"))
	case d == 1:
		return then.Format(i18n.G("tomorrow at 15:04 MST"))
	case d > 1:
		// TRANSLATORS: %d will be at least 2; the singular is only included to help gettext
		return fmt.Sprintf(then.Format(i18n.NG("in %d day, at 15:04 MST", "in %d days, at 15:04 MST", d)), d)
	default:
		// the following message is brought to you by Joel Armando, the self-described awesome and sexy mathematician.
		panic("you have broken the law of trichotomy! ℤ is no longer totally ordered! chaos ensues!")
	}
}
