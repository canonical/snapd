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
	"io/ioutil"
	"os"
	"time"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/usersession/client"
)

type inhibitionFlow interface {
	Notify() error
	Finish() error
}

type textFlow struct {
	snapName string

	pb progress.Meter
}

func (tf *textFlow) Notify() error {
	_, err := fmt.Fprintf(Stdout, i18n.G("snap package %q is being refreshed, please wait"), tf.snapName)
	if err != nil {
		return err
	}
	tf.pb = progress.MakeProgressBar(Stdout)
	tf.pb.Spin(i18n.G("please wait..."))
	return nil
}

func (tf *textFlow) Finish() error {
	if tf.pb != nil {
		tf.pb.Finished()
	}
	return nil
}

type graphicalFlow struct {
	snapName string

	notifiedDesktopIntegration bool
}

func (gf *graphicalFlow) Notify() error {
	gf.notifiedDesktopIntegration = tryNotifyRefreshViaSnapDesktopIntegrationFlow(gf.snapName)
	if gf.notifiedDesktopIntegration {
		return nil
	}

	// unable to use snapd-desktop-integration, let's fallback graphical session flow
	refreshInfo := client.PendingSnapRefreshInfo{
		InstanceName: gf.snapName,
		// remaining time = 0 results in "Snap .. is refreshing now" message from
		// usersession agent.
		TimeRemaining: 0,
	}
	return pendingRefreshNotification(&refreshInfo)
}

func (gf *graphicalFlow) Finish() error {
	if gf.notifiedDesktopIntegration {
		// snapd-desktop-integration detects inhibit unlock itself, do nothing
		return nil
	}

	// finish graphical session flow
	finishRefreshInfo := client.FinishedSnapRefreshInfo{InstanceName: gf.snapName}
	return finishRefreshNotification(&finishRefreshInfo)
}

var tryNotifyRefreshViaSnapDesktopIntegrationFlow = func(snapName string) (notified bool) {
	// Check if Snapd-Desktop-Integration is available
	conn, err := dbusutil.SessionBus()
	if err != nil {
		logger.Noticef("unable to connect dbus session: %v", err)
		return false
	}
	obj := conn.Object("io.snapcraft.SnapDesktopIntegration", "/io/snapcraft/SnapDesktopIntegration")
	extraParams := make(map[string]dbus.Variant)
	err = obj.Call("io.snapcraft.SnapDesktopIntegration.ApplicationIsBeingRefreshed", 0, snapName, runinhibit.HintFile(snapName), extraParams).Store()
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

var pendingRefreshNotification = func(refreshInfo *client.PendingSnapRefreshInfo) error {
	userclient := client.NewForUids(os.Getuid())
	if err := userclient.PendingRefreshNotification(context.TODO(), refreshInfo); err != nil {
		return err
	}
	return nil
}

var finishRefreshNotification = func(refreshInfo *client.FinishedSnapRefreshInfo) error {
	userclient := client.NewForUids(os.Getuid())
	if err := userclient.FinishRefreshNotification(context.TODO(), refreshInfo); err != nil {
		return err
	}
	return nil
}

func newInhibitionFlow(snapName string) inhibitionFlow {
	if isGraphicalSession() {
		return &graphicalFlow{snapName: snapName}
	}
	return &textFlow{snapName: snapName}
}

// waitWhileInhibited blocks until the snap is not inhibited anymore.
//
// Calling clean() releases the lock and finishes notification flow.
//
// IMPORTANT: It is the callers responsibility to call clean().
func waitWhileInhibited(snapName string) (clean func() error, retErr error) {
	flow := newInhibitionFlow(snapName)

	// every 0.5s check if the inhibition file is still present.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	notified := false
	for {
		hintFlock, err := runinhibit.OpenLock(snapName)
		if err != nil {
			return nil, err
		}
		// release lock if we return early with an error
		defer func() {
			// keep lock if no errors occur
			if retErr != nil {
				hintFlock.Close()
			}
		}()

		// hold read lock
		if err := hintFlock.ReadLock(); err != nil {
			return nil, err
		}
		// read inhibition hint
		hint, err := hintFromFile(hintFlock.File())
		if err != nil {
			return nil, err
		}

		switch hint {
		case runinhibit.HintInhibitedForRefresh:
			if !notified {
				// only notify once
				if err := flow.Notify(); err != nil {
					return nil, err
				} else {
					notified = true
				}
			}
		case runinhibit.HintNotInhibited:
			clean := func() error {
				defer hintFlock.Close()
				if err := flow.Finish(); err != nil {
					return err
				}
				return nil
			}

			return clean, nil
		}

		// release lock to allow changes during waiting interval
		hintFlock.Close()
		<-ticker.C
	}
}

var hintFromFile = func(hintFile *os.File) (runinhibit.Hint, error) {
	buf, err := ioutil.ReadAll(hintFile)
	if err != nil {
		return "", err
	}
	return runinhibit.Hint(string(buf)), nil
}
