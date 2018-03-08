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

package main

import (
	"time"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/timeutil"
)

var timeutilHuman = timeutil.Human

type timeMixin struct {
	AbsTime bool `long:"abs-time"`
}

var timeDescs = mixinDescs{
	"abs-time": i18n.G("Display absolute times (in RFC 3339 format). Otherwise, display relative times up to 60 days, then YYYY-MM-DD."),
}

func (mx timeMixin) fmtTime(t time.Time) string {
	if mx.AbsTime {
		return t.Format(time.RFC3339)
	}
	return timeutilHuman(t)
}
