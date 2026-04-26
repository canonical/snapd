// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package auth

import (
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/testutil"
)

func MockSeclogLogUserCreated(f func(seclog.SnapdUser)) (restore func()) {
	return testutil.Mock(&seclogLogUserCreated, f)
}

func MockSeclogLogUserUpdated(f func(seclog.SnapdUser, []string)) (restore func()) {
	return testutil.Mock(&seclogLogUserUpdated, f)
}

func MockSeclogLogUserRemoved(f func(seclog.SnapdUser)) (restore func()) {
	return testutil.Mock(&seclogLogUserRemoved, f)
}
