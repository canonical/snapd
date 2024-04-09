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
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/usersession/client"
)

var runinhibitWaitWhileInhibited = runinhibit.WaitWhileInhibited

// errSnapRefreshConflict indicates that a retry is needed because snap-run
// might have started without a hint lock file and now there is an ongoing refresh
// which could alter the current snap revision.
var errSnapRefreshConflict = fmt.Errorf("snap refresh conflict detected")

// maybeWaitWhileInhibited is a wrapper for waitWhileInhibited that skips waiting
// if refresh-app-awareness flag is disabled.
func maybeWaitWhileInhibited(ctx context.Context, snapName string, appName string) (info *snap.Info, app *snap.AppInfo, hintFlock *osutil.FileLock, err error) {
	// wait only if refresh-app-awareness flag is enabled
	if features.RefreshAppAwareness.IsEnabled() {
		return waitWhileInhibited(ctx, snapName, appName)
	}

	info, app, err = getInfoAndApp(snapName, appName, snap.R(0))
	if err != nil {
		return nil, nil, nil, err
	}
	return info, app, nil, nil
}

// waitWhileInhibited blocks until snap is not inhibited for refresh anymore and then
// returns a locked hint file lock along with the latest snap and app information.
// If the snap is inhibited for refresh, a notification flow is initiated during
// the inhibition period.
//
// NOTE: A snap without a hint file is considered not inhibited and a nil FileLock is returned.
//
// NOTE: It is the caller's responsibility to release the returned file lock.
func waitWhileInhibited(ctx context.Context, snapName string, appName string) (info *snap.Info, app *snap.AppInfo, hintFlock *osutil.FileLock, err error) {
	flow := newInhibitionFlow(snapName)
	notified := false
	notInhibited := func(ctx context.Context) (err error) {
		// Get updated "current" snap info.
		info, app, err = getInfoAndApp(snapName, appName, snap.R(0))
		// We might have started without a hint lock file and we have an
		// ongoing refresh which removed current symlink.
		if errors.As(err, &snap.NotFoundError{}) {
			// Race condition detected
			logger.Debugf("%v", err)
			return errSnapRefreshConflict
		}
		return err
	}
	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		if !notified {
			info, app, err = getInfoAndApp(snapName, appName, inhibitInfo.Previous)
			if err != nil {
				return false, err
			}
			// Don't wait, continue with old revision.
			if app.IsService() {
				return true, nil
			}
			// Don't start flow if we are not inhibited for refresh.
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

	// If the snap is inhibited from being used then postpone running it until
	// that condition passes.
	hintFlock, err = runinhibitWaitWhileInhibited(ctx, snapName, notInhibited, inhibited, 500*time.Millisecond)
	if err != nil {
		// It is fine to return an error here without finishing the notification
		// flow because we either failed because of it or before it, so it
		// should not have started in the first place.
		return nil, nil, nil, err
	}

	if notified {
		if err := flow.FinishInhibitionNotification(ctx); err != nil {
			hintFlock.Close()
			return nil, nil, nil, err
		}
	}

	return info, app, hintFlock, nil
}

func getInfoAndApp(snapName, appName string, rev snap.Revision) (*snap.Info, *snap.AppInfo, error) {
	info, err := getSnapInfo(snapName, rev)
	if err != nil {
		return nil, nil, err
	}

	app, exists := info.Apps[appName]
	if !exists {
		return nil, nil, fmt.Errorf(i18n.G("cannot find app %q in %q"), appName, snapName)
	}

	return info, app, nil
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
