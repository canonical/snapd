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
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var MinLane = minLane

func NewAndAddRoutes() (*Daemon, error) {
	d, err := New()
	if err != nil {
		return nil, err
	}
	d.addRoutes()
	return d, nil
}

func NewWithOverlord(o *overlord.Overlord) *Daemon {
	d := &Daemon{overlord: o, state: o.State()}
	d.addRoutes()
	return d
}

func (d *Daemon) RouterMatch(req *http.Request, m *mux.RouteMatch) bool {
	return d.router.Match(req, m)
}

func (d *Daemon) Overlord() *overlord.Overlord {
	return d.overlord
}

func (d *Daemon) RequestedRestart() state.RestartType {
	return d.requestedRestart
}

func MockUcrednetGet(mock func(remoteAddr string) (pid int32, uid uint32, socket string, err error)) (restore func()) {
	oldUcrednetGet := ucrednetGet
	ucrednetGet = mock
	return func() {
		ucrednetGet = oldUcrednetGet
	}
}

func MockEnsureStateSoon(mock func(*state.State)) (original func(*state.State), restore func()) {
	oldEnsureStateSoon := ensureStateSoon
	ensureStateSoon = mock
	return ensureStateSoonImpl, func() {
		ensureStateSoon = oldEnsureStateSoon
	}
}

func MockMuxVars(vars func(*http.Request) map[string]string) (restore func()) {
	old := muxVars
	muxVars = vars
	return func() {
		muxVars = old
	}
}

func MockShutdownTimeout(tm time.Duration) (restore func()) {
	old := shutdownTimeout
	shutdownTimeout = tm
	return func() {
		shutdownTimeout = old
	}
}

func MockSnapstateInstall(mock func(context.Context, *state.State, string, *snapstate.RevisionOptions, int, snapstate.Flags) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateInstall := snapstateInstall
	snapstateInstall = mock
	return func() {
		snapstateInstall = oldSnapstateInstall
	}
}

type (
	Resp            = resp
	ErrorResult     = errorResult
	SnapInstruction = snapInstruction
)

func (inst *snapInstruction) Dispatch() snapActionFunc {
	return inst.dispatch()
}
