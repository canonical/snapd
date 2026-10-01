// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package internal

import (
	"fmt"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/client"
)

var (
	GenerateOnCalendarSchedules = generateOnCalendarSchedules
)

const (
	MaxLenUnixAbstractSocketAddress = maxLenUnixAbstractSocketAddress
	MaxLenUnixPathSocketAddress     = maxLenUnixPathSocketAddress
)

func MockUserSessionQueryServiceStatusMany(f func(units []string) (map[int][]client.ServiceUnitStatus, map[int][]client.ServiceFailure, error)) (restore func()) {
	restore = testutil.Backup(&userSessionQueryServiceStatusMany)
	userSessionQueryServiceStatusMany = func(units []string) (map[int][]*systemd.UnitStatus, error) {
		statuses, failures, err := f(units)
		if err != nil {
			return nil, err
		}
		for uid, fs := range failures {
			if len(fs) > 0 {
				return nil, fmt.Errorf("cannot query status for uid %d: %s", uid, fs[0].Error)
			}
		}
		result := make(map[int][]*systemd.UnitStatus)
		for uid, sts := range statuses {
			result[uid] = nil
			for _, st := range sts {
				result[uid] = append(result[uid], st.SystemdUnitStatus())
			}
		}
		return result, nil
	}
	return restore
}
