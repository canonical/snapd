/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"path/filepath"
	"time"

	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/systemd"
)

// PurgeFlags can be used to pass additional flags to the snap removal request
type PurgeFlags uint

const (
	// DoPurgeActive requests that the data files of an active
	// package be removed. Without this that is disallowed.
	DoPurgeActive PurgeFlags = 1 << iota
)

var remove = removeSnapData

// Purge a part by a partSpec string, name[.namespace][=version]
func Purge(partSpec string, flags PurgeFlags, meter progress.Meter) error {
	var e error
	datadirs := DataDirs(partSpec)
	if len(datadirs) == 0 {
		return ErrPackageNotFound
	}

	purgeActive := flags&DoPurgeActive != 0

	var active []*SnapPart

	for _, datadir := range datadirs {
		yamlPath := filepath.Join(snapAppsDir, datadir.Dirname(), datadir.Version, "meta", "package.yaml")
		part, err := NewInstalledSnapPart(yamlPath, datadir.Namespace)
		if err != nil {
			// no such part installed
			continue
		}
		if part.IsActive() {
			if !purgeActive {
				return ErrStillActive
			}
			active = append(active, part)
		}
	}

	sysd := systemd.New(globalRootDir, meter)
	var start []string
	for _, pkg := range active {
		for _, svc := range pkg.Services() {
			fn := filepath.Base(generateServiceFileName(pkg.m, svc))
			if err := sysd.Stop(fn, time.Duration(svc.StopTimeout)); err != nil {
				meter.Notify(fmt.Sprintf("service %s failed to stop: %s", fn, err))
			} else {
				start = append(start, fn)
			}
		}

	}

	for _, datadir := range datadirs {
		if err := remove(datadir.Dirname(), datadir.Version); err != nil {
			e = err
			meter.Notify(fmt.Sprintf("unable to purge %s version %s: %s", datadir.Dirname(), datadir.Version, err.Error()))
		}
	}

	for _, fn := range start {
		if err := sysd.Start(fn); err != nil {
			meter.Notify(fmt.Sprintf("service %s failed to start: %s", fn, err))
		}
	}

	return e
}
