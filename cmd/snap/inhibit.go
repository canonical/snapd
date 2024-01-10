// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/usersession/client"
)

var runinhibitWaitWhileInhibited = runinhibit.WaitWhileInhibited

func waitWhileInhibited(ctx context.Context, snapName string) error {
	flow := newInhibitionFlow(snapName)
	notified := false
	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		if !notified {
			// wait for HintInhibitedForRefresh set by gate-auto-refresh hook handler
			// when it has finished; the hook starts with HintInhibitedGateRefresh lock
			// and then either unlocks it or changes to HintInhibitedForRefresh (see
			// gateAutoRefreshHookHandler in hooks.go).
			if hint != runinhibit.HintInhibitedForRefresh {
				return false, nil
			}
			// Start notification flow.
			if err := flow.StartInhibitionNotification(ctx); err != nil {
				return true, err
			}
			// Make sure we call notification flow only once.
			notified = true
		}
		return false, nil
	}

	hintFlock, err := runinhibitWaitWhileInhibited(ctx, snapName, nil, inhibited, 500*time.Millisecond)
	if err != nil {
		// It is fine to return an error here without finishing the notification
		// flow because we either failed because of it or before it, so it
		// should not have started in the first place.
		return err
	}

	// XXX: closing as we don't need it for now, this lock will be used in a later iteration
	if hintFlock != nil {
		hintFlock.Close()
	}

	if notified {
		if err := flow.FinishInhibitionNotification(ctx); err != nil {
			return err
		}
	}

	return nil
}

type inhibitionFlow interface {
	StartInhibitionNotification(ctx context.Context) error
	FinishInhibitionNotification(ctx context.Context) error
}

var newInhibitionFlow = func(instanceName string) inhibitionFlow {
	if isGraphicalSession() {
		return &graphicalFlow{instanceName: instanceName}
	}
	return &textFlow{instanceName: instanceName}
}

type textFlow struct {
	instanceName string
}

func (tf *textFlow) StartInhibitionNotification(ctx context.Context) error {
	_, err := fmt.Fprintf(Stdout, i18n.G("snap package %q is being refreshed, please wait\n"), tf.instanceName)
	// TODO: add proper progress spinner
	return err
}

func (tf *textFlow) FinishInhibitionNotification(ctx context.Context) error {
	return nil
}

type graphicalFlow struct {
	instanceName string

	notifiedDesktopIntegration bool
}

func (gf *graphicalFlow) StartInhibitionNotification(ctx context.Context) error {
	gf.notifiedDesktopIntegration = tryNotifyRefreshViaSnapDesktopIntegrationFlow(ctx, gf.instanceName)
	if gf.notifiedDesktopIntegration {
		return nil
	}

	// unable to use snapd-desktop-integration, let's fall back to graphical session flow
	refreshInfo := client.PendingSnapRefreshInfo{
		InstanceName: gf.instanceName,
		// remaining time = 0 results in "Snap .. is refreshing now" message from
		// usersession agent.
		TimeRemaining: 0,
	}
	return pendingRefreshNotification(ctx, &refreshInfo)
}

func (gf *graphicalFlow) FinishInhibitionNotification(ctx context.Context) error {
	if gf.notifiedDesktopIntegration {
		// snapd-desktop-integration detects inhibit unlock itself, do nothing
		return nil
	}

	// finish graphical session flow
	finishRefreshInfo := client.FinishedSnapRefreshInfo{InstanceName: gf.instanceName}
	return finishRefreshNotification(ctx, &finishRefreshInfo)
}

var tryNotifyRefreshViaSnapDesktopIntegrationFlow = func(ctx context.Context, snapName string) (notified bool) {
	// Check if Snapd-Desktop-Integration is available
	conn, err := dbusutil.SessionBus()
	if err != nil {
		logger.Noticef("unable to connect dbus session: %v", err)
		return false
	}
	obj := conn.Object("io.snapcraft.SnapDesktopIntegration", "/io/snapcraft/SnapDesktopIntegration")
	extraParams := make(map[string]dbus.Variant)
	err = obj.CallWithContext(ctx, "io.snapcraft.SnapDesktopIntegration.ApplicationIsBeingRefreshed", 0, snapName, runinhibit.HintFile(snapName), extraParams).Store()
	if err != nil {
		logger.Noticef("unable to successfully call io.snapcraft.SnapDesktopIntegration.ApplicationIsBeingRefreshed: %v", err)
		return false
	}
	return true
}

var isGraphicalSession = func() bool {
	// TODO: uncomment once there is a proper UX review
	//return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	return false
}

var pendingRefreshNotification = func(ctx context.Context, refreshInfo *client.PendingSnapRefreshInfo) error {
	userclient := client.NewForUids(os.Getuid())
	if err := userclient.PendingRefreshNotification(ctx, refreshInfo); err != nil {
		return err
	}
	return nil
}

var finishRefreshNotification = func(ctx context.Context, refreshInfo *client.FinishedSnapRefreshInfo) error {
	userclient := client.NewForUids(os.Getuid())
	if err := userclient.FinishRefreshNotification(ctx, refreshInfo); err != nil {
		return err
	}
	return nil
}
