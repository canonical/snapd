// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package agentnotify

import (
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	userclient "github.com/snapcore/snapd/usersession/client"
)

var (
	NotifyAgentOnLinkageChange                 = notifyAgentOnLinkageChange
	MaybeSendClientFinishedRefreshNotification = maybeSendClientFinishRefreshNotification
)

func MockMaybeSendClientFinishRefreshNotification(f func(*state.State, *snapstate.SnapSetup)) (restore func()) {
	r := testutil.Backup(&maybeSendClientFinishRefreshNotification)
	maybeSendClientFinishRefreshNotification = f
	return r
}

func MockAsyncFinishRefreshNotification(f func(*userclient.FinishedSnapRefreshInfo)) (restore func()) {
	r := testutil.Backup(&asyncFinishRefreshNotification)
	asyncFinishRefreshNotification = f
	return r
}

func MockHasActiveConnection(fn func(st *state.State, iface string) (bool, error)) (restore func()) {
	old := snapstate.HasActiveConnection
	snapstate.HasActiveConnection = fn
	return func() {
		snapstate.HasActiveConnection = old
	}
}
