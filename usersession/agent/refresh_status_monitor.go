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

package agent

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/godbus/dbus"
	"github.com/mvo5/goconfigparser"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	userclient "github.com/snapcore/snapd/usersession/client"
)

// notifyRefreshToSnapDesktopIntegration interacts with snapd-desktop-integration daemon, to show or hide
// a progress dialog.
func notifyRefreshToSnapDesktopIntegration(snapName string, desktopEntry string, operation NotifyRefreshOperation) error {
	// Check if Snapd-Desktop-Integration is available
	conn, err := dbusutil.SessionBus()
	if err != nil {
		return fmt.Errorf("unable to connect dbus session: %v", err)
	}
	obj := conn.Object("io.snapcraft.SnapDesktopIntegration", "/io/snapcraft/SnapDesktopIntegration")
	extraParams := make(map[string]dbus.Variant)
	if desktopEntry != "" {
		parser := goconfigparser.New()
		desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, desktopEntry+".desktop")
		if err := parser.ReadFile(desktopFilePath); err == nil {
			icon, _ := parser.Get("Desktop Entry", "Icon")
			extraParams["icon_image"] = dbus.MakeVariant(icon)
		}
	}
	if operation == DestroyNotification {
		err = obj.Call("io.snapcraft.SnapDesktopIntegration.ApplicationRefreshCompleted", 0, snapName, extraParams).Store()
	} else {
		err = obj.Call("io.snapcraft.SnapDesktopIntegration.ApplicationIsBeingRefreshed", 0, snapName, "", extraParams).Store()
	}
	if err != nil {
		return fmt.Errorf("unable to successfully call io.snapcraft.SnapDesktopIntegration: %v", err)
	}
	return nil
}

// updateRefreshStatusDesktopIntegration interacts with snapd-desktop-integration daemon, to update the
// status of a progress dialog (setting the new percentage of progress)
func updateRefreshStatusDesktopIntegration(snapName string, barText string, progress float64) error {
	// Check if Snapd-Desktop-Integration is available
	conn, err := dbusutil.SessionBus()
	if err != nil {
		return fmt.Errorf("unable to connect dbus session: %v", err)
	}
	obj := conn.Object("io.snapcraft.SnapDesktopIntegration", "/io/snapcraft/SnapDesktopIntegration")
	extraParams := make(map[string]dbus.Variant)
	if progress > 1 {
		err = obj.Call("io.snapcraft.SnapDesktopIntegration.ApplicationRefreshPulsed", 0, snapName, barText, extraParams).Store()
	} else {
		err = obj.Call("io.snapcraft.SnapDesktopIntegration.ApplicationRefreshPercentage", 0, snapName, barText, progress, extraParams).Store()
	}
	if err != nil {
		return fmt.Errorf("unable to successfully call io.snapcraft.SnapDesktopIntegration to update the state: %v", err)
	}
	return nil
}

func sliceContains(slice []string, element string) bool {
	for _, item := range slice {
		if item == element {
			return true
		}
	}
	return false
}

// monitorChanges is called from the REST API. It receives the Change ID and the Task IDs
// for a refresh operation, and monitors it using the snapd API.
func monitorChanges(refreshInfo userclient.BeginDeferredRefreshNotificationInfo, notificationMgr notification.NotificationManager) {
	// First, get a reference to the Change API
	var cliConfig client.Config
	percentage := 0.0
	barText := ""
	cli := client.New(&cliConfig)

	// Now, send the notification to inform the user that the snap will be refreshed
	// If snapd-desktop-integration is not installed, notifyRefreshToSnapDesktopIntegration will
	// fail. In that case, use a standard desktop notification.
	if err := notifyRefreshToSnapDesktopIntegration(refreshInfo.InstanceName, refreshInfo.AppDesktopEntry, ShowNewNotification); err != nil {
		// TODO: this message needs to be crafted better as it's the only thing guaranteed to be delivered.
		summary := fmt.Sprintf(i18n.G("Updating “%s” snap"), refreshInfo.InstanceName)
		body := i18n.G("Please wait before opening it.")
		sendDesktopStandardNotification(notificationMgr, refreshInfo, summary, body)
	}

	// Now, ask periodically for the status of the Change
	for {
		<-time.After(500 * time.Millisecond)
		change, err := cli.Change(refreshInfo.ChangeId)
		if err != nil {
			logger.Noticef("Failed to get the change with ID %s", refreshInfo.ChangeId)
			continue
		}
		totalTasks := 0.0
		doneTasks := 0.0
		msg := ""
		// sort the tasks by its ID
		sort.SliceStable(change.Tasks, func(i, j int) bool {
			return change.Tasks[i].ID < change.Tasks[j].ID
		})
		// count how many tasks belong to this snap refresh operation, how
		// may have already been done, and which is the first one being done or
		// waiting to begin (this is the text that will be shown in the
		// snapd-desktop-integration progress bar)
		for _, task := range change.Tasks {
			if !sliceContains(refreshInfo.TaskIDs, task.ID) {
				continue
			}
			totalTasks++
			if task.Status != state.DoStatus.String() && task.Status != state.DoingStatus.String() {
				doneTasks++
			} else {
				if msg == "" {
					msg = task.Summary
				}
			}
		}
		newPercentage := doneTasks / totalTasks
		// If the percentage of tasks done or the summary of the current task has changed,
		// send an update to snapd-desktop-integration
		if msg != barText || newPercentage != percentage {
			barText = msg
			percentage = newPercentage
			newText := fmt.Sprintf("%s (%d/%d)", barText, int(doneTasks), int(totalTasks))
			updateRefreshStatusDesktopIntegration(refreshInfo.InstanceName, newText, percentage)
		}
		if totalTasks == doneTasks {
			break
		}
	}

	// this will just close the "working on it" window, so we also must show an extra message
	// to the user notifying they that the update has concluded. This is even more important
	// in the case where the user closed the "working on it" window.
	if err := notifyRefreshToSnapDesktopIntegration(refreshInfo.InstanceName, "", DestroyNotification); err != nil {
		logger.Noticef("Failed to communicate with Snapd-Desktop-Integration to close a refresh popup: %v", err)
	}
	// TODO: this message needs to be crafted better as it's the only thing guaranteed to be delivered.
	summary := fmt.Sprintf(i18n.G("Refreshed “%s” snap"), refreshInfo.InstanceName)
	body := i18n.G("Ready to launch.")

	sendDesktopStandardNotification(notificationMgr, refreshInfo, summary, body)
}
