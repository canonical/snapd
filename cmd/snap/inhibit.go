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
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

var runinhibitWaitWhileInhibited = runinhibit.WaitWhileInhibited

// errSnapRefreshConflict indicates that a retry is needed because snap-run
// might have started without a hint lock file and now there is an ongoing refresh
// which could alter the current snap revision.
var errSnapRefreshConflict = fmt.Errorf("snap refresh conflict detected")

// maybeWaitWhileInhibited is a wrapper for waitWhileInhibited that skips waiting
// if refresh-app-awareness flag is disabled.
func maybeWaitWhileInhibited(ctx context.Context, cli *client.Client, snapName string, appName string) (info *snap.Info, app *snap.AppInfo, hintFlock *osutil.FileLock, err error) {
	// wait only if refresh-app-awareness flag is enabled
	if features.RefreshAppAwareness.IsEnabled() {
		return waitWhileInhibited(ctx, cli, snapName, appName)
	}

	info, app = mylog.Check3(getInfoAndApp(snapName, appName, snap.R(0)))

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
func waitWhileInhibited(ctx context.Context, cli *client.Client, snapName string, appName string) (info *snap.Info, app *snap.AppInfo, hintFlock *osutil.FileLock, err error) {
	var flow inhibitionFlow
	notified := false
	notInhibited := func(ctx context.Context) (err error) {
		// Get updated "current" snap info.
		info, app = mylog.Check3(getInfoAndApp(snapName, appName, snap.R(0)))
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
			flow = newInhibitionFlow(cli, snapName)
			info, app = mylog.Check3(getInfoAndApp(snapName, appName, inhibitInfo.Previous))

			// Don't wait, continue with old revision.
			if app.IsService() {
				return true, nil
			}
			// Don't start flow if we are not inhibited for refresh.
			if hint != runinhibit.HintInhibitedForRefresh {
				return false, nil
			}
			mylog.Check(
				// Start notification flow.
				flow.StartInhibitionNotification(ctx))

			// Make sure we call notification flow only once.
			notified = true
		}
		return false, nil
	}

	// If the snap is inhibited from being used then postpone running it until
	// that condition passes.
	hintFlock = mylog.Check2(runinhibitWaitWhileInhibited(ctx, snapName, notInhibited, inhibited, 500*time.Millisecond))

	// It is fine to return an error here without finishing the notification
	// flow because we either failed because of it or before it, so it
	// should not have started in the first place.

	if notified {
		mylog.Check(flow.FinishInhibitionNotification(ctx))
	}

	return info, app, hintFlock, nil
}

func getInfoAndApp(snapName, appName string, rev snap.Revision) (*snap.Info, *snap.AppInfo, error) {
	info := mylog.Check2(getSnapInfo(snapName, rev))

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

var newInhibitionFlow = func(cli *client.Client, instanceName string) inhibitionFlow {
	return &noticesFlow{instanceName: instanceName, cli: cli}
}

type noticesFlow struct {
	instanceName string

	cli *client.Client
}

func (gf *noticesFlow) StartInhibitionNotification(ctx context.Context) error {
	opts := client.NotifyOptions{
		Type: client.SnapRunInhibitNotice,
		Key:  gf.instanceName,
	}
	_ := mylog.Check2(gf.cli.Notify(&opts))

	// Fallback to text notification if marker "snap-refresh-observe"
	// interface is not connected and a terminal is detected.
	if isStdoutTTY && !markerInterfaceConnected(gf.cli) {
		fmt.Fprintf(Stderr, i18n.G("snap package %q is being refreshed, please wait\n"), gf.instanceName)
	}

	return nil
}

func (gf *noticesFlow) FinishInhibitionNotification(ctx context.Context) error {
	// snapd-desktop-integration (or any other client) should detect that the
	// snap is no longer inhibited by itself, do nothing.
	return nil
}

func markerInterfaceConnected(cli *client.Client) bool {
	// Check if marker interface "snap-refresh-observe" is connected.
	connOpts := client.ConnectionOptions{
		Interface: "snap-refresh-observe",
	}
	connections := mylog.Check2(cli.Connections(&connOpts))

	// Ignore error (maybe snapd is being updated) and fallback to
	// text flow instead.

	if len(connections.Established) == 0 {
		// Marker interface is not connected.
		// No snap (i.e. snapd-desktop-integration) is listening, let's fallback
		// to text flow.
		return false
	}
	return true
}
