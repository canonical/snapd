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

// inotifyWatcher manages the inotify watcher, allowing to have a single watch descriptor open
type inotifyWatcher struct {
	// The watch object
	wd *inotify.Watcher
	// This is used to ensure that the watcher goroutine is launched only once
	doOnce sync.Once
	// This channel is used to add a new CGroup that has to be checked
	addWatchChan chan *groupToWatch
	// Contains the list of groups to monitor, to detect when they have been deleted
	groupList []*groupToWatch
	// Contains the list of paths being monitored by the inotify watcher.
	// The paths monitored aren't the CGroup paths, but the parent ones, because
	// in /sys/fs is not possible to detect the InDeleteSelf message, only the
	// InDelete one, so we must monitor the parent folder and detect when a
	// specific folder, corresponding to a CGroup, has been deleted.
	// Each path has associated an integer, which is an usage counter. Every
	// time a new CGroup paths have to be monitored, the parent paths are
	// checked in the map, and if they already exist, the usage counter is
	// incremented by one (of course, if a parent path isn't in the map,
	// it is initialized to 'one'). Every time the path of a CGroup is deleted,
	// the corresponding usage counter here corresponding to the parent path
	// is decremented, and when it reaches zero, it is removed and inotify is
	// notified to stop monitoring it.
	pathList map[string]int32
}

// Contains the data corresponding to a CGroup that must be watched to detect
// when it is destroyed
type groupToWatch struct {
	// This is the CGroup identifier
	name string
	// This contains a list of folders to monitor. When all of them have been
	// deleted, the CGroup has been destroyed and there are no processes running
	folders []string
	// This channel is used to notify to the requester that this CGroup has been
	// destroyed. The watcher writes the CGroup identifier on it; this way, the
	// same channel can be shared to monitor several CGroups.
	channel chan<- string
}

var currentWatcher *inotifyWatcher = &inotifyWatcher{
	wd:           nil,
	pathList:     make(map[string]int32),
	addWatchChan: make(chan *groupToWatch),
}

func (iw *inotifyWatcher) addWatch(newWatch *groupToWatch) {
	var folderList []string
	for _, fullPath := range newWatch.folders {
		// It's not possible to use inotify.InDeleteSelf in /sys/fs because it
		// isn't triggered, so we must monitor the parent folder and use InDelete
		basePath := path.Dir(fullPath)
		if _, exists := iw.pathList[basePath]; !exists {
			iw.pathList[basePath] = 0
			if err := iw.wd.AddWatch(basePath, inotify.InDelete); err != nil {
				delete(iw.pathList, basePath)
				continue
			}
		}
		iw.pathList[fullPath]++
		if osutil.FileExists(fullPath) {
			folderList = append(folderList, fullPath)
		} else {
			iw.removePath(fullPath)
		}
	}
	if len(folderList) == 0 {
		newWatch.channel <- newWatch.name
	} else {
		newWatch.folders = folderList
		iw.groupList = append(iw.groupList, newWatch)
	}
}

func (iw *inotifyWatcher) removePath(fullPath string) {
	iw.pathList[fullPath]--
	if iw.pathList[fullPath] == 0 {
		iw.wd.RemoveWatch(fullPath)
		delete(iw.pathList, fullPath)
	}
}

func (iw *inotifyWatcher) processEvent(watch *groupToWatch, event *inotify.Event) bool {
	var tmpFolders []string
	// every time an "InDelete" event is received, check if this CGroup is waiting for
	// that file/folder deletion event
	for _, fullPath := range watch.folders {
		if fullPath != event.Name {
			tmpFolders = append(tmpFolders, fullPath)
		} else {
			// if the folder name is in the list of folders to monitor, decrement the
			// parent's usage counter, and remove it from the list of folders to watch
			// in this CGroup (by not adding it to tmpFolders)
			iw.removePath(fullPath)
		}
	}
	watch.folders = tmpFolders
	if len(tmpFolders) == 0 {
		// if all the files/folders of this CGroup have been deleted, notify the
		// client that it is done.
		watch.channel <- watch.name
		// By returning FALSE, the caller is notified to remove this CGroup from
		// the list of watched CGroups.
		return false
	}
	// Returning TRUE means that this CGroup still have files/folders to be
	// monitored, and must be kept in the CGroup list.
	return true
}

func (iw *inotifyWatcher) watcherMainLoop() {
	for {
		select {
		case event := <-iw.wd.Event:
			if event.Mask&inotify.InDelete == 0 {
				continue
			}
			var newGroupList []*groupToWatch
			for _, watch := range iw.groupList {
				if iw.processEvent(watch, event) {
					newGroupList = append(newGroupList, watch)
				}
			}
			iw.groupList = newGroupList
		case newWatch := <-iw.addWatchChan:
			iw.addWatch(newWatch)
		}
	}
}

// MonitorDelete monitors the specified paths for deletion.
// Once all of them have been deleted, it pushes the specified name through the channel.
func (iw *inotifyWatcher) monitorDelete(folders []string, name string, channel chan<- string) error {
	iw.doOnce.Do(func() {
		wd, err := inotify.NewWatcher()
		if err == nil {
			iw.wd = wd
			go iw.watcherMainLoop()
		}
	})

	if iw.wd == nil {
		return errors.New("cannot initialise Inotify.")
	}
	iw.addWatchChan <- &groupToWatch{
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
	return currentWatcher.monitorDelete(paths, snapName, channel)
}
