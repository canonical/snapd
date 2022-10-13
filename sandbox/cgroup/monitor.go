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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup/inotify"
)

// snapAppMonitorState contains all the data to monitor a specific Snap:
// its name, the list of paths to monitor, and the channel to send
// the notification when all the paths have been deleted.
type snapAppMonitorState struct {
	name                          string
	cgroupPaths                   []string
	npaths                        int
	allDeletedNotificationChannel chan string
}

// CGroupMonitor allows to monitor several CGroups, detect when all
// the running instances of each one have been closed, and notify
// them separately.
type CGroupMonitor struct {
	// The key is the base path of all the files/folders being monitored
	// this way, paths 'XXXX/folder1' and 'XXXX/file2' would be monitored
	// using a single inotify call for 'XXXX' folder, and their respective
	// snapAppMonitorState structs would be stored in the 'XXXX' key of this
	// map.
	monitored                map[string][]*snapAppMonitorState
	watcher                  *inotify.Watcher
	requestNewMonitorChannel chan snapAppMonitorState
}

var currentCGroupMonitor = CGroupMonitor{
	watcher:                  nil,
	requestNewMonitorChannel: make(chan snapAppMonitorState),
	monitored:                make(map[string][]*snapAppMonitorState),
}

func (this *CGroupMonitor) onFilesDeleted(filename string) {
	basePath := path.Dir(filename)
	appWatchers := this.monitored[basePath]
	var newList []*snapAppMonitorState
	for _, app := range appWatchers {
		for _, folder := range app.cgroupPaths {
			if folder == filename {
				app.npaths--
			}
		}
		if app.npaths == 0 {
			// all the folders have disappeared, so notify that this app has no more instances running
			app.allDeletedNotificationChannel <- app.name
		} else {
			newList = append(newList, app)
		}
	}
	if len(newList) != 0 {
		this.monitored[basePath] = newList
	} else {
		delete(this.monitored, basePath)
		this.watcher.RemoveWatch(basePath)
	}
}

func (this *CGroupMonitor) onFilesAdded(newApp *snapAppMonitorState) {
	if newApp.npaths == 0 {
		newApp.allDeletedNotificationChannel <- newApp.name
		return
	}
	addedPaths := false
	for _, fullPath := range newApp.cgroupPaths {
		basePath := path.Dir(fullPath) // Monitor the path containing this folder
		_, exists := this.monitored[basePath]
		if exists {
			continue
		}
		err := this.watcher.AddWatch(basePath, inotify.InDelete)
		if err != nil {
			continue
		}
		if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
			// if the file/folder to monitor doesn't exist after the parent being added, remove it
			this.watcher.RemoveWatch(basePath)
			continue
		}
		this.monitored[basePath] = append(this.monitored[basePath], newApp)
		addedPaths = true
	}
	if !addedPaths {
		// if the files/folders to monitor don't exist, send the notification now
		newApp.allDeletedNotificationChannel <- newApp.name
	}
}

func (this *CGroupMonitor) monitorMainLoop() {
	for {
		select {
		case event := <-this.watcher.Event:
			if event.Mask&inotify.InDelete != 0 {
				this.onFilesDeleted(event.Name)
			}
		case newApp := <-this.requestNewMonitorChannel:
			this.onFilesAdded(&newApp)
		}
	}
}

// MonitorSnap is the method to call to monitor the running instances of an specific Snap.
// It receives the name of the snap to monitor (for example, "firefox" or "steam")
// and a channel. The caller can wait on the channel, and when all the instances of
// the specific snap have ended, the name of the snap will be sent through the channel.
// This allows to use the same channel to monitor several snaps
func MonitorSnap(snapName string, channel chan string) bool {
	options := InstancePathsOptions{
		returnCGroupPath: true,
	}
	paths, _ := InstancePathsOfSnap(snapName, options)
	return MonitorFiles(snapName, paths, channel)
}

// MonitorFiles allows to monitor a group of files/folders
// and, when all of them have been deleted, emits the specified name through the channel.
func MonitorFiles(name string, folders []string, channel chan string) bool {
	if currentCGroupMonitor.watcher == nil {
		wd, err := inotify.NewWatcher()
		if err != nil {
			logger.Noticef("Failed to open file watcher")
			return true
		}
		currentCGroupMonitor.watcher = wd
		go currentCGroupMonitor.monitorMainLoop()
	}

	data := snapAppMonitorState{
		name:                          name,
		cgroupPaths:                   folders,
		allDeletedNotificationChannel: channel,
		npaths:                        len(folders),
	}
	currentCGroupMonitor.requestNewMonitorChannel <- data
	return false
}

// NumberOfWaitingMonitors is currently used for testing. It returns the number of folders being
// monitored. This may not match the number of paths passed in MonitorFiles, because
// the main loop monitors the parent folder, so if several monitored files/folders
// are in the same parent folder, they will count as only one for this method.
func NumberOfWaitingMonitors() int {
	return len(currentCGroupMonitor.monitored)
}
