// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package cgroup

import (
	"errors"
	"os"
	"path"

	"github.com/snapcore/snapd/sandbox/cgroup/inotify"
)

// appMonitorData contains all the data to monitor a specific Snap:
// its name, the list of paths to monitor, and the channel to send
// the notification when all the paths have been deleted.
type appMonitorData struct {
	name        string
	cgroupPaths []string
	npaths      int
	channel     chan string
}

// CGroupMonitor allows to monitor several CGroups, detect when all
// the running instances of each one have been closed, and notify
// them separately. It should be considered a singleton, and
// obtained using DefaultCGroupMonitor().
type CGroupMonitor struct {
	watched map[string][]*appMonitorData
	watcher *inotify.Watcher
	channel chan appMonitorData
}

var currentCGroupMonitor = CGroupMonitor{
	watcher: nil,
	channel: make(chan appMonitorData),
	watched: make(map[string][]*appMonitorData),
}

func onFilesDeleted(filename string) {
	basePath := path.Dir(filename)
	appWatchers := currentCGroupMonitor.watched[basePath]
	var newList []*appMonitorData
	for _, app := range appWatchers {
		for _, folder := range app.cgroupPaths {
			if folder == filename {
				app.npaths--
			}
		}
		if app.npaths == 0 {
			// all the folders have disappeared, so notify that this app has no more instances running
			app.channel <- app.name
		} else {
			newList = append(newList, app)
		}
	}
	if len(newList) != 0 {
		currentCGroupMonitor.watched[basePath] = newList
	} else {
		delete(currentCGroupMonitor.watched, basePath)
		currentCGroupMonitor.watcher.RemoveWatch(basePath)
	}
}

func onFilesAdded(newApp *appMonitorData) {
	if newApp.npaths == 0 {
		newApp.channel <- newApp.name
		return
	}
	addedPaths := false
	for _, fullPath := range newApp.cgroupPaths {
		basePath := path.Dir(fullPath) // Monitor the path containing this folder
		_, exists := currentCGroupMonitor.watched[basePath]
		if exists {
			continue
		}
		err := currentCGroupMonitor.watcher.AddWatch(basePath, inotify.InDelete)
		if err != nil {
			continue
		}
		if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
			// if the file/folder to monitor doesn't exist after the parent being added, remove it
			currentCGroupMonitor.watcher.RemoveWatch(basePath)
			continue
		}
		currentCGroupMonitor.watched[basePath] = append(currentCGroupMonitor.watched[basePath], newApp)
		addedPaths = true
	}
	if !addedPaths {
		// if the files/folders to monitor don't exist, send the notification now
		newApp.channel <- newApp.name
	}
}

func monitorMainLoop() {
	for {
		select {
		case event := <-currentCGroupMonitor.watcher.Event:
			if event.Mask&inotify.InDelete != 0 {
				onFilesDeleted(event.Name)
			}
		case newApp := <-currentCGroupMonitor.channel:
			onFilesAdded(&newApp)
		}
	}
}

// DefaultCGroupMonitor launches the main loop and returns the CGroup singleton
func DefaultCGroupMonitor() *CGroupMonitor {
	if currentCGroupMonitor.watcher == nil {
		wd, err := inotify.NewWatcher()
		if err != nil {
			return nil
		}
		currentCGroupMonitor.watcher = wd
		go monitorMainLoop()
	}
	return &currentCGroupMonitor
}

// MonitorSnap is the method to call to monitor the running instances of an specific Snap.
// It receives the name of the snap to monitor (for example, "firefox" or "steam")
// and a channel. The caller can wait on the channel, and when all the instances of
// the specific snap have ended, the name of the snap will be sent through the channel.
// This allows to use the same channel to monitor several snaps
func (this CGroupMonitor) MonitorSnap(snapName string, channel chan string) {
	paths, _ := InstancePathsOfSnap(snapName, InstancePathsFlagsOnlyPaths)
	data := appMonitorData{
		name:        snapName,
		cgroupPaths: paths,
		channel:     channel,
		npaths:      len(paths),
	}
	this.channel <- data
}

// MonitorFiles is currently used for testing. It allows to monitor a group of files/folders
// and, when all of them have been deleted, emits the specified name through the channel.
func (this CGroupMonitor) MonitorFiles(name string, folders []string, channel chan string) {
	data := appMonitorData{
		name:        name,
		cgroupPaths: folders,
		channel:     channel,
		npaths:      len(folders),
	}
	this.channel <- data
}

// NumberOfWaitingMonitors is currently used for testing. It returns the number of folders being
// watched. This may not match the number of paths passed in MonitorFiles, because
// the main loop monitors the parent folder, so if several monitored files/folders
// are in the same parent folder, they will count as only one for this method.
func (this CGroupMonitor) NumberOfWaitingMonitors() int {
	return len(this.watched)
}
