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
	"github.com/snapcore/snapd/sandbox/cgroup/inotify"
)

type inotifyWatcher struct {
	wd        *inotify.Watcher
	addWatch  chan *groupToWatch
	groupList []*groupToWatch
	pathList  map[string]int32
}

type groupToWatch struct {
	name    string
	folders []string
	channel chan string
}

var currentWatcher *inotifyWatcher = &inotifyWatcher{
	wd:       nil,
	pathList: make(map[string]int32),
	addWatch: make(chan *groupToWatch),
}

func addWatch(newWatch *groupToWatch) {
	var folderList []string
	for _, fullPath := range newWatch.folders {
		if _, exists := currentWatcher.pathList[fullPath]; !exists {
			currentWatcher.pathList[fullPath] = 0
			if err := currentWatcher.wd.AddWatch(fullPath, inotify.InDeleteSelf); err != nil {
				delete(currentWatcher.pathList, fullPath)
				continue
			}
			folderList = append(folderList, fullPath)
		}
		currentWatcher.pathList[fullPath]++
	}
	if len(folderList) == 0 {
		newWatch.channel <- newWatch.name
	} else {
		newWatch.folders = folderList
		currentWatcher.groupList = append(currentWatcher.groupList, newWatch)
	}
}

func processEvent(watch *groupToWatch, event *inotify.Event) bool {
	var tmpFolders []string
	for _, fullPath := range watch.folders {
		if fullPath != event.Name {
			tmpFolders = append(tmpFolders, fullPath)
		} else {
			currentWatcher.pathList[fullPath]--
			if currentWatcher.pathList[fullPath] == 0 {
				currentWatcher.wd.RemoveWatch(fullPath)
				delete(currentWatcher.pathList, fullPath)
			}
		}
	}
	watch.folders = tmpFolders
	if len(tmpFolders) == 0 {
		watch.channel <- watch.name
		return false
	}
	return true
}

func watcherMainLoop() {
	for {
		select {
		case event := <-currentWatcher.wd.Event:
			if event.Mask&inotify.InDeleteSelf == 0 {
				continue
			}
			var newGroupList []*groupToWatch
			for _, watch := range currentWatcher.groupList {
				if processEvent(watch, event) {
					newGroupList = append(newGroupList, watch)
				}
			}
			currentWatcher.groupList = newGroupList
		case newWatch := <-currentWatcher.addWatch:
			addWatch(newWatch)
		}
	}
}

// MonitorFullDelete allows to monitor a group of files/folders
// and, when all of them have been deleted, emits the specified name through the channel.
func monitorFullDelete(name string, folders []string, channel chan string) error {
	if currentWatcher.wd == nil {
		wd, err := inotify.NewWatcher()
		if err != nil {
			return err
		}
		currentWatcher.wd = wd
		go watcherMainLoop()
	}

	currentWatcher.addWatch <- &groupToWatch{
		name:    name,
		folders: folders,
		channel: channel,
	}
	return nil
}

// MonitorSnapEnded is the method to call to monitor the running instances of an specific Snap.
// It receives the name of the snap to monitor (for example, "firefox" or "steam")
// and a channel. The caller can wait on the channel, and when all the instances of
// the specific snap have ended, the name of the snap will be sent through the channel.
// This allows to use the same channel to monitor several snaps
func MonitorSnapEnded(snapName string, channel chan string) error {
	options := InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	paths, _ := InstancePathsOfSnap(snapName, options)
	return monitorFullDelete(snapName, paths, channel)
}
