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
	"github.com/snapcore/snapd/strutil"
)

// MonitorFiles allows to monitor a group of files/folders
// and, when all of them have been deleted, emits the specified name through the channel.
func MonitorFiles(name string, folders []string, channel chan string) bool {
	wd, err := inotify.NewWatcher()
	if err != nil {
		logger.Noticef("Failed to open file watcher")
		return true
	}

	var toMonitor []string
	var tmpFolders []string

	for _, fullPath := range folders {
		basePath := path.Dir(fullPath)
		if !strutil.ListContains(toMonitor, basePath) {
			err := wd.AddWatch(basePath, inotify.InDelete)
			if err != nil {
				logger.Noticef("Failed to add a watcher for folder %s", basePath)
				continue
			}
			toMonitor = append(toMonitor, basePath)
		}
		// add first the parent folder to the monitor, and only then check if the
		// child file/folder does exist. This ensures that there is no race
		// condition if the file/folder is removed between both steps.
		if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
			continue
		}
		tmpFolders = append(tmpFolders, fullPath)
	}
	folders = tmpFolders

	go func() {
		defer func() {
			wd.Close()
		}()
		for len(folders) != 0 {
			event := <-wd.Event
			if event.Mask&inotify.InDelete == 0 {
				continue
			}
			var tmpFolders []string
			for _, fullPath := range folders {
				if fullPath != event.Name {
					tmpFolders = append(tmpFolders, fullPath)
				}
			}
			folders = tmpFolders
		}
		channel <- name
	}()
	return false
}

// MonitorSnap is the method to call to monitor the running instances of an specific Snap.
// It receives the name of the snap to monitor (for example, "firefox" or "steam")
// and a channel. The caller can wait on the channel, and when all the instances of
// the specific snap have ended, the name of the snap will be sent through the channel.
// This allows to use the same channel to monitor several snaps
func MonitorSnap(snapName string, channel chan string) bool {
	options := InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	paths, _ := InstancePathsOfSnap(snapName, options)
	return MonitorFiles(snapName, paths, channel)
}
