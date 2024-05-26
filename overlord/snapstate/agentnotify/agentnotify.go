// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"context"
	"errors"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	userclient "github.com/snapcore/snapd/usersession/client"
)

func init() {
	snapstate.AddLinkSnapParticipant(snapstate.LinkSnapParticipantFunc(notifyAgentOnLinkageChange))
}

func notifyAgentOnLinkageChange(st *state.State, snapsup *snapstate.SnapSetup) error {
	instanceName := snapsup.InstanceName()

	var snapst snapstate.SnapState
	if mylog.Check(snapstate.Get(st, instanceName, &snapst)); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !snapst.IsInstalled() {
		// nothing to do
		return nil
	}

	// the only use-case right now is snaps going from inactive->active
	// for continued-auto-refreshes
	if snapst.Active {
		return notifyLinkSnap(snapsup)
	}
	return nil
}

func notifyLinkSnap(snapsup *snapstate.SnapSetup) error {
	// Note that we only show a notification here if the refresh was
	// triggered by a "continued-auto-refresh", i.e. when the user
	// closed an application that had a auto-refresh ready.
	if snapsup.Flags.IsContinuedAutoRefresh {
		logger.Debugf("notifying user client about continued refresh for %q", snapsup.InstanceName())
		sendClientFinishRefreshNotification(snapsup)
	}

	return nil
}

var sendClientFinishRefreshNotification = func(snapsup *snapstate.SnapSetup) {
	refreshInfo := &userclient.FinishedSnapRefreshInfo{
		InstanceName: snapsup.InstanceName(),
	}
	client := userclient.New()
	// run in a go-routine to avoid potentially slow operation
	go func() {
		mylog.Check(client.FinishRefreshNotification(context.TODO(), refreshInfo))
	}()
}
