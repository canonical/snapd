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
	"github.com/snapcore/snapd/snap"
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

func MockUnsafeReadSnapInfo(mock func(string) (*snap.Info, error)) (restore func()) {
	oldUnsafeReadSnapInfo := unsafeReadSnapInfo
	unsafeReadSnapInfo = mock
	return func() {
		unsafeReadSnapInfo = oldUnsafeReadSnapInfo
	}
}

func MockAssertstateRefreshSnapDeclarations(mock func(*state.State, int) error) (restore func()) {
	oldAssertstateRefreshSnapDeclarations := assertstateRefreshSnapDeclarations
	assertstateRefreshSnapDeclarations = mock
	return func() {
		assertstateRefreshSnapDeclarations = oldAssertstateRefreshSnapDeclarations
	}
}

func MockSnapstateInstall(mock func(context.Context, *state.State, string, *snapstate.RevisionOptions, int, snapstate.Flags) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateInstall := snapstateInstall
	snapstateInstall = mock
	return func() {
		snapstateInstall = oldSnapstateInstall
	}
}

func MockSnapstateInstallPath(mock func(*state.State, *snap.SideInfo, string, string, string, snapstate.Flags) (*state.TaskSet, *snap.Info, error)) (restore func()) {
	oldSnapstateInstallPath := snapstateInstallPath
	snapstateInstallPath = mock
	return func() {
		snapstateInstallPath = oldSnapstateInstallPath
	}
}

func MockSnapstateTryPath(mock func(*state.State, string, string, snapstate.Flags) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateTryPath := snapstateTryPath
	snapstateTryPath = mock
	return func() {
		snapstateTryPath = oldSnapstateTryPath
	}
}

func MockSnapstateInstallMany(mock func(*state.State, []string, int) ([]string, []*state.TaskSet, error)) (restore func()) {
	oldSnapstateInstallMany := snapstateInstallMany
	snapstateInstallMany = mock
	return func() {
		snapstateInstallMany = oldSnapstateInstallMany
	}
}

func MockSnapstateUpdateMany(mock func(context.Context, *state.State, []string, int, *snapstate.Flags) ([]string, []*state.TaskSet, error)) (restore func()) {
	oldSnapstateUpdateMany := snapstateUpdateMany
	snapstateUpdateMany = mock
	return func() {
		snapstateUpdateMany = oldSnapstateUpdateMany
	}
}

func MockSnapstateRemoveMany(mock func(*state.State, []string) ([]string, []*state.TaskSet, error)) (restore func()) {
	oldSnapstateRemoveMany := snapstateRemoveMany
	snapstateRemoveMany = mock
	return func() {
		snapstateRemoveMany = oldSnapstateRemoveMany
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

func (inst *snapInstruction) DispatchForMany() snapsActionFunc {
	return inst.dispatchForMany()
}
