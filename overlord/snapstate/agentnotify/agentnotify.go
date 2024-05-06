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

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
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
	if err := snapstate.Get(st, instanceName, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !snapst.IsInstalled() {
		// nothing to do
		return nil
	}

	// the only use-case right now is snaps going from inactive->active
	// for continued-auto-refreshes
	if snapst.Active {
		return notifyLinkSnap(st, snapsup)
	}
	return nil
}

func notifyLinkSnap(st *state.State, snapsup *snapstate.SnapSetup) error {
	// Note that we only show a notification here if the refresh was
	// triggered by a "continued-auto-refresh", i.e. when the user
	// closed an application that had a auto-refresh ready.
	if snapsup.Flags.IsContinuedAutoRefresh {
		logger.Debugf("notifying user client about continued refresh for %q", snapsup.InstanceName())
		sendClientFinishRefreshNotification(st, snapsup)
	}

	return nil
}

var asyncFinishRefreshNotification = func(refreshInfo *userclient.FinishedSnapRefreshInfo) {
	client := userclient.New()
	// run in a go-routine to avoid potentially slow operation
	go func() {
		if err := client.FinishRefreshNotification(context.TODO(), refreshInfo); err != nil {
			logger.Noticef("cannot send finish refresh notification: %v", err)
		}
	}()
}

var sendClientFinishRefreshNotification = func(st *state.State, snapsup *snapstate.SnapSetup) {
	tr := config.NewTransaction(st)
	experimentalRefreshAppAwarenessUX, err := features.Flag(tr, features.RefreshAppAwarenessUX)
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("Cannot send notification about pending refresh: %v", err)
		return
	}
	if experimentalRefreshAppAwarenessUX {
		// use notices + warnings fallback flow instead
		return
	}

	markerExists, err := snapstate.HasActiveConnection(st, "snap-refresh-observe")
	if err != nil {
		logger.Noticef("Cannot send notification about pending refresh: %v", err)
		return
	}
	if markerExists {
		// found snap with marker interface, skip notification
		return
	}
	refreshInfo := &userclient.FinishedSnapRefreshInfo{
		InstanceName: snapsup.InstanceName(),
	}
	asyncFinishRefreshNotification(refreshInfo)
}
