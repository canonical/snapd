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

	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
)

func (b Backend) CopyData(newSnap, oldSnap *snap.Info, meter progress.Meter) error {
	// deal with the old data or
	// otherwise just create a empty data dir

	// Make sure the common data directory exists, even if this isn't a new
	// install.
	if err := os.MkdirAll(newSnap.CommonDataDir(), 0755); err != nil {
		return err
	}

	if oldSnap == nil {
		return os.MkdirAll(newSnap.DataDir(), 0755)
	}

	return copySnapData(oldSnap, newSnap)
}

func (b Backend) UndoCopyData(newInfo *snap.Info, oldInfo *snap.Info, meter progress.Meter) error {
	err1 := RemoveSnapData(newInfo)
	// XXX: log

	var err2 error
	if oldInfo == nil {
		// first install, remove created common dir
		err2 = RemoveSnapCommonData(newInfo)
		// XXX: log
	}

	return firstErr(err1, err2)
}
