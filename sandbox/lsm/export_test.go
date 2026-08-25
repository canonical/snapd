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

package lsm

import (
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/selinux"
	"github.com/snapcore/snapd/testutil"
)

func MockApparmorSecurityLabelFromPid(f func(int) (string, error)) (restore func()) {
	return testutil.Mock(&apparmorSecurityLabelFromPid, f)
}

func MockSELinuxSecurityLabelFromPid(f func(int) (string, error)) (restore func()) {
	return testutil.Mock(&selinuxSecurityLabelFromPid, f)
}

func MockApparmorProbedLevel(f func() apparmor.LevelType) (restore func()) {
	return testutil.Mock(&apparmorProbedLevel, f)
}

func MockSELinuxProbedLevel(f func() selinux.LevelType) (restore func()) {
	return testutil.Mock(&selinuxProbedLevel, f)
}
