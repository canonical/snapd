// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

package daemon

import (
	"os/user"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func MockServicestateControl(f func(st *state.State, appInfos []*snap.AppInfo, inst *servicestate.Instruction, cu *user.User, flags *servicestate.Flags, context *hookstate.Context) ([]*state.TaskSet, error)) (restore func()) {
	old := servicestateControl
	servicestateControl = f
	return func() {
		servicestateControl = old
	}
}

type (
	AppInfoOptions = appInfoOptions
)

var (
	SplitAppName        = splitAppName
	AppInfosFor         = appInfosFor
	AppInfoServiceTrue  = appInfoOptions{service: true}
	AppInfoServiceFalse = appInfoOptions{}
)
