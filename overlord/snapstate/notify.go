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

	"github.com/snapcore/snapd/logger"
	userclient "github.com/snapcore/snapd/usersession/client"
)

// XXX: make this a separate package? and pass e.g. userclient.PendingRefreshInfo instead of snapsup?

// UnlinkCurrentSnapStarted is called when the "unlink-current-snap" starts
// running for real. This typcially means that a refresh is happening.
func notifyUnlinkCurrentSnapStarted(snapsup *SnapSetup) error {
	if snapsup.Flags.IsContinuedAutoRefresh {
		// TODO: send notification that the refresh starts and
		// pass the change-id too so that a future snapd-observe
		// can be used to monitor the change
	}
	return nil
}

// LinkSnapFinished is called when "link-snap" has finished. This means
// the snap is ready to use.
func notifyLinkSnapFinished(snapsup *SnapSetup) error {
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
