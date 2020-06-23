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

package daemon

import (
	"net/http"
	"time"

	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type Resp = resp
type ErrorResult = errorResult

var MinLane = minLane

func NewWithOverlord(o *overlord.Overlord) *Daemon {
	d := &Daemon{overlord: o}
	d.addRoutes()
	return d
}

func MockMuxVars(vars func(*http.Request) map[string]string) (restore func()) {
	old := muxVars
	muxVars = vars
	return func() {
		muxVars = old
	}
}

func MockBuildID(mock string) (restore func()) {
	old := buildID
	buildID = mock
	return func() {
		buildID = old
	}
}

func MockShutdownTimeout(tm time.Duration) (restore func()) {
	old := shutdownTimeout
	shutdownTimeout = tm
	return func() {
		shutdownTimeout = old
	}
}

func MockServicestateControl(f func(st *state.State, appInfos []*snap.AppInfo, inst *servicestate.Instruction, context *hookstate.Context) ([]*state.TaskSet, error)) (restore func()) {
	old := servicestateControl
	servicestateControl = f
	return func() {
		servicestateControl = old
	}
}
