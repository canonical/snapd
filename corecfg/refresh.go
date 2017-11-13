// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg

import (
	"github.com/snapcore/snapd/timeutil"
)

func validateRefreshSchedule(tr Conf) error {
	refreshScheduleStr, err := coreCfg(tr, "refresh.schedule")
	if err != nil {
		return err
	}
	if refreshScheduleStr == "" {
		return nil
	}
	// FIXME: add support for things like "managed" once we have that
	_, err = timeutil.ParseSchedule(refreshScheduleStr)
	return err
}
