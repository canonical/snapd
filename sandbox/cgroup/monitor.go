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
	"path"
	"sync"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/inotify"
)

type inotifyWatcher struct {
	wd           *inotify.Watcher
	doOnce       sync.Once
	addWatchChan chan *groupToWatch
	// groupList and pathList are accessed only from inside the watcherMainLoop,
	// so no locks are needed.
	groupList []*groupToWatch
	pathList  map[string]int32
}

type groupToWatch struct {
	name    string
	folders []string
	channel chan string
}

var currentWatcher *inotifyWatcher = &inotifyWatcher{
	wd:           nil,
	pathList:     make(map[string]int32),
	addWatchChan: make(chan *groupToWatch),
}

func (this *inotifyWatcher) addWatch(newWatch *groupToWatch) {
	var folderList []string
	for _, fullPath := range newWatch.folders {
		// It's not possible to use inotify.InDeleteSelf in /sys/fs because it
		// isn't triggered, so we must monitor the parent folder and use InDelete
		basePath := path.Dir(fullPath)
		if _, exists := this.pathList[basePath]; !exists {
			this.pathList[basePath] = 0
			if err := this.wd.AddWatch(basePath, inotify.InDelete); err != nil {
				delete(this.pathList, basePath)
				continue
			}
		}
		this.pathList[fullPath]++
		if osutil.FileExists(fullPath) {
			folderList = append(folderList, fullPath)
		} else {
			this.removePath(fullPath)
		}
	}
	if len(folderList) == 0 {
		newWatch.channel <- newWatch.name
	} else {
		newWatch.folders = folderList
		this.groupList = append(this.groupList, newWatch)
	}
}

func (this *inotifyWatcher) removePath(fullPath string) {
	this.pathList[fullPath]--
	if this.pathList[fullPath] == 0 {
		this.wd.RemoveWatch(fullPath)
		delete(this.pathList, fullPath)
	}
}

func (this *inotifyWatcher) processEvent(watch *groupToWatch, event *inotify.Event) bool {
	var tmpFolders []string
	for _, fullPath := range watch.folders {
		if fullPath != event.Name {
			tmpFolders = append(tmpFolders, fullPath)
		} else {
			this.removePath(fullPath)
		}
	}
	watch.folders = tmpFolders
	if len(tmpFolders) == 0 {
		watch.channel <- watch.name
		return false
	}
	return true
}

func (this *inotifyWatcher) watcherMainLoop() {
	for {
		select {
		case event := <-this.wd.Event:
			if event.Mask&inotify.InDelete == 0 {
				continue
			}
			var newGroupList []*groupToWatch
			for _, watch := range this.groupList {
				if this.processEvent(watch, event) {
					newGroupList = append(newGroupList, watch)
				}
			}
			this.groupList = newGroupList
		case newWatch := <-this.addWatchChan:
			this.addWatch(newWatch)
		}
	}
}

// MonitorFullDelete allows to monitor a group of files/folders
// and, when all of them have been deleted, emits the specified name through the channel.
func (this *inotifyWatcher) monitorFullDelete(name string, folders []string, channel chan string) error {
	this.doOnce.Do(func() {
		wd, err := inotify.NewWatcher()
		if err == nil {
			this.wd = wd
			go this.watcherMainLoop()
		}
	})

	if this.wd == nil {
		return errors.New("Inotify failed to initialize")
	}
	this.addWatchChan <- &groupToWatch{
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
	return currentWatcher.monitorFullDelete(snapName, paths, channel)
}
