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

// calculateTaskProgress search the task list and count how many tasks remain
// to be completed for the snap that is being refreshed, to be able to show in
// the popup dialog how many tasks have been already been done.
// Also, it takes the name of the task being done right now to show it in the
// dialog.
//
// It must be called with st locked. It isn't locked/unlocked inside to allow
// to call it from asyncRefreshOnSnapClose(), where it is already locked.
/*func calculateTaskProgress(st *state.State, instanceName string, revision snap.Revision) (totalTasks int, remainingTasks int, curTaskSummary string) {

	remainingTasks = 0
	totalTasks = 0
	curTaskSummary = ""
	tasks := make([]*state.Task, 0)
	//var err error
	//var id int

	st.Lock()
	for _, task := range st.Tasks() {
		snapsup, _, err := snapSetupAndState(task)
		if err != nil {
			continue
		}
		if snapsup.InstanceName() != instanceName || snapsup.Revision() != revision {
			continue
		}
		tasks = append(tasks, task)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].ID() < tasks[j].ID()
	})
	for _, task := range tasks {
		totalTasks++
		if task.Status() != state.DoStatus && task.Status() != state.DoingStatus {
			continue
		}
		// For some reason, the order of the tasks changes every time the
		// function is called, so we can't just take the name of the first
		// task with state.DoingStatus or state.DoStatus, because it would
		// change between checks. This means that, sometimes, we will miss
		// the name of the task because it was too fast and, in that
		// precise moment, no one was in "DoingStatus".
		if curTaskSummary == "" && task.Status() != state.DoneStatus {
			curTaskSummary = task.Summary()
		}
		remainingTasks++
	}
	st.Unlock()
	return totalTasks, remainingTasks, curTaskSummary
}*/

/*var monitorRefreshChange = func(instanceName string, revision snap.Revision, desktopPath string, id string) {
	for {
		<-time.After(250 * time.Millisecond)
		st.Lock()
		for _, change := range st.Changes() {
			if change != changeFound {
				continue
			}
			changeFound = change
			ctasks := 0
			for _, task := range changeFound.Tasks() {
				if task.Status() != state.DoStatus && task.Status() != state.DoingStatus {
					continue
				}
				snapsup, _, err := snapSetupAndState(task)
				if err != nil {
					continue
				}
				if snapsup.InstanceName() != instanceName || snapsup.Revision() != revision {
					continue
				}
				ctasks++
			}
			if ctasks == 0 && len(changeFound.Tasks()) != 0 {
				completed = true
				break
			}
			if ctasks != ntasks {
				ntasks = ctasks
				fmt.Println("Tasks for", snapName, ", Change id", changeFound.ID(), "(", changeFound.Kind(), ")", ntasks, "/", len(changeFound.Tasks()))
				for _, task := range changeFound.Tasks() {
					if task.Status() != state.DoStatus && task.Status() != state.DoingStatus {
						continue
					}
					snapsup, _, err := snapSetupAndState(task)
					if err != nil {
						continue
					}
					if snapsup.InstanceName() != instanceName || snapsup.Revision() != revision {
						continue
					}
					fmt.Println("Task", task.ID(), task.Kind(), task.Summary())
				}
			}
		}
		st.Unlock()
		if completed {
			break
		}
	}
}*/

// notifyBeginRefresh waits for the specific change to appear and then
// uses either snapd-desktop-integration (if available)
// or the standard desktop notifications to notify the user that a refresh
// is in progress, and when it has been completed and they can re-launch
// the application.
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
	asyncBeginDeferredRefreshNotification(context.TODO(), userclient.New(), &deferredRefreshInfo)

	/*deferredRefreshInfo := userclient.FinishedSnapRefreshInfo{InstanceName: instanceName, AppDesktopEntry: desktopPath}
	clientConnection := userclient.New()
	var currentChange *state.Change = nil

	// Notify to the user session that this snap is being refreshed.
	// If snapd-desktop-integration is installed, it will show a popup with a progress bar,
	// which will be refreshed with the current progress of the operation. If it's not
	// installed, it will show a classic notification to inform to the user that the snap
	// is going to be refreshed right now.
	asyncBeginDeferredRefreshNotification(context.TODO(), clientConnection, &deferredRefreshInfo)

	// Find the auto-refresh change corresponding to this
	for {
		changes := st.Changes()
		for _, change := range changes {
			if change.Kind() != "auto-refresh" || (change.Status() != state.DoStatus && change.Status() != state.DoingStatus) {
				continue
			}
			currentChange = change
			break
		}
		if currentChange != nil {
			fmt.Println("Found change:", currentChange.Summary())
			break
		}
		<-time.After(500 * time.Millisecond)
	}

	clientRefreshStatus := userclient.SnapRefreshProgressInfo{InstanceName: instanceName}
	// periodically, check how many tasks remain and the name of the current task, and call
	// SnapRefreshProgressNotification to send this data to the user session. This will update
	// the progress bar in the popup from snapd-desktop-integration, allowing the user to know
	// how the refresh process is going.

	lastBarText := ""
	for {
		totalTasks, remainingTasks, newBarText := calculateTaskProgress(st, instanceName, revision)
		newValue := 1 - (float64(remainingTasks) / float64(totalTasks))
		if remainingTasks == 0 {
			// Once there are no more remaining tasks, notify the user session. It will close the
			// snapd-desktop-integration popup and show a standard notification indicating to the user
			// that the snap has been refreshed and that can be opened now.
			asyncFinishDeferredRefreshNotification(context.TODO(), clientConnection, &deferredRefreshInfo)
			break
		}
		if newValue != clientRefreshStatus.Percentage || (newBarText != "" && newBarText != lastBarText) {
			if newBarText != "" {
				lastBarText = newBarText
			}
			clientRefreshStatus.Percentage = newValue
			clientRefreshStatus.BarText = fmt.Sprintf("%s (%d/%d)", lastBarText, totalTasks-remainingTasks+1, totalTasks)
			asyncSnapRefreshProgressNotification(context.TODO(), clientConnection, &clientRefreshStatus)
		}
		<-time.After(500 * time.Millisecond)
	}*/
}
