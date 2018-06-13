// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package backend

import (
	"os"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
)

// CopySnapData makes a copy of oldSnap data for newSnap in its data directories.
func (b Backend) CopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error {
	// deal with the old data or
	// otherwise just create a empty data dir

	// Make sure the common data directory exists, even if this isn't a new
	// install.
	if err := os.MkdirAll(newSnap.CommonDataDir(), 0755); err != nil {
		return err
	}

	if oldSnap == nil {
		return os.MkdirAll(newSnap.DataDir(), 0755)
	} else if oldSnap.Revision == newSnap.Revision {
		// nothing to do
		return nil
	}

	return copySnapData(oldSnap, newSnap)
}

// UndoCopySnapData removes the copy that may have been done for newInfo snap of oldInfo snap data and also the data directories that may have been created for newInfo snap.
func (b Backend) UndoCopySnapData(newInfo *snap.Info, oldInfo *snap.Info, meter progress.Meter) error {
	if oldInfo != nil && oldInfo.Revision == newInfo.Revision {
		// nothing to do
		return nil
	}
	err1 := b.RemoveSnapData(newInfo)
	if err1 != nil {
		logger.Noticef("Cannot remove data directories for %q: %v", newInfo.InstanceName(), err1)
	}

	var err2 error
	if oldInfo == nil {
		// first install, remove created common data dir
		err2 = b.RemoveSnapCommonData(newInfo)
		if err2 != nil {
			logger.Noticef("Cannot remove common data directories for %q: %v", newInfo.InstanceName(), err2)
		}
	} else {
		err2 = b.untrashData(newInfo)
		if err2 != nil {
			logger.Noticef("Cannot restore original data for %q while undoing: %v", newInfo.InstanceName(), err2)
		}
	}

	return firstErr(err1, err2)
}

// ClearTrashedData removes the trash. It returns no errors on the assumption that it is called very late in the game.
func (b Backend) ClearTrashedData(oldSnap *snap.Info) {
	dirs, err := snapDataDirs(oldSnap)
	if err != nil {
		logger.Noticef("Cannot remove previous data for %q: %v", oldSnap.InstanceName(), err)
		return
	}

	for _, d := range dirs {
		if err := clearTrash(d); err != nil {
			logger.Noticef("Cannot remove %s: %v", d, err)
		}
	}
}
