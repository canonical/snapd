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

package snapstate

import (
	"context"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	userclient "github.com/snapcore/snapd/usersession/client"
)

// asyncBeginDeferredRefreshNotification broadcasts a desktop notification in a goroutine,
// telling the user that a refresh process is being started, and that they must wait before
// trying to launch the snap.
//
// This allows the, possibly slow, communication with each snapd session agent,
// to be performed without holding the snap state lock.
var asyncBeginDeferredRefreshNotification = func(context context.Context, client *userclient.Client, refreshInfo *userclient.BeginDeferredRefreshNotificationInfo) {
	go func() {
		if err := client.BeginDeferredRefreshNotification(context, refreshInfo); err != nil {
			logger.Noticef("Cannot send notification about finishing deferred refresh: %v", err)
		}
	}()
}

// notifyBeginRefresh finds the specific CHANGE element of this refresh, to get its ID.
// When it is found, it notifies the user daemon the CHANGE ID and all the
// task IDs belonging to this specific snap (because in the same change there can
// be tasks for other snaps if there were several snaps with pending refreshes and
// all of them are closed at the same time).
var notifyBeginRefresh = func(st *state.State, instanceName string, revision snap.Revision, snapName string, desktopPath string) {

	var changeFound *state.Change = nil

	for {
		st.Lock()
		// Find the change for this snap
		for _, change := range st.Changes() {
			if change.Status() != state.DoingStatus && change.Status() != state.DoStatus {
				continue
			}
			if change.Kind() != "auto-refresh" {
				continue
			}
			for _, task := range change.Tasks() {
				snapsup, _, err := snapSetupAndState(task)
				if err != nil {
					continue
				}
				if snapsup.InstanceName() != instanceName || snapsup.Revision() != revision {
					continue
				}
				changeFound = change
				break
			}
		}
		st.Unlock()
		if changeFound != nil {
			break
		}
		<-time.After(500 * time.Millisecond)
	}

	// Now find all the tasks that belong to this snap
	st.Lock()
	deferredRefreshInfo := userclient.BeginDeferredRefreshNotificationInfo{
		AppName:         snapName,
		InstanceName:    instanceName,
		Revision:        revision.String(),
		ChangeId:        changeFound.ID(),
		AppDesktopEntry: desktopPath}
	for _, task := range changeFound.Tasks() {
		snapsup, _, err := snapSetupAndState(task)
		if err != nil {
			continue
		}
		if snapsup.InstanceName() != instanceName || snapsup.Revision() != revision {
			continue
		}
		deferredRefreshInfo.TaskIDs = append(deferredRefreshInfo.TaskIDs, task.ID())
	}
	st.Unlock()
	// Notify the user daemon that there is an autorefresh, and which Change ID and which Task IDs
	// to monitor
	asyncBeginDeferredRefreshNotification(context.TODO(), userclient.New(), &deferredRefreshInfo)
}
