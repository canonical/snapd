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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	userclient "github.com/snapcore/snapd/usersession/client"
)

// XXX: make this a separate package?
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

	if snapst.Active {
		return notifyLinkSnap(snapsup)
	} else {
		return notifyUnlinkSnap(snapsup)
	}
	return nil
}

func notifyUnlinkSnap(snapsup *snapstate.SnapSetup) error {
	// TODO: send notification that the refresh starts and
	// pass the change-id too so that a future snapd-observe
	// can be used to monitor the change
	return nil
}

func notifyLinkSnap(snapsup *snapstate.SnapSetup) error {
	// Note that we only show a notification here if the refresh was
	// triggered by a "continued-auto-refresh", i.e. when the user
	// closed an application that had a auto-refresh ready.
	if snapsup.Flags.IsContinuedAutoRefresh {
		// XXX: run this as a go-routine?
		refreshInfo := &userclient.FinishedSnapRefreshInfo{
			InstanceName: snapsup.InstanceName(),
		}
		client := userclient.New()
		if err := client.FinishRefreshNotification(context.TODO(), refreshInfo); err != nil {
			logger.Noticef("cannot send finish refresh notification: %v", err)
		}
	}

	return nil
}
