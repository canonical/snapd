// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package overlord

import (
	"os"
	"syscall"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
)

type overlordStateBackend struct {
	path           string
	ensureBefore   func(d time.Duration)
	requestRestart func(t state.RestartType)
}

type debugStat struct {
	Ino   uint64
	Size  uint64
	Mtime time.Time
	Ctime time.Time
}

func grabDebugStat(p string, when string) (db debugStat, ok bool) {
	oldFi, err := os.Stat(p)
	if err == nil {
		sysFi := oldFi.Sys().(*syscall.Stat_t)
		db.Ino = sysFi.Ino
		db.Size = uint64(sysFi.Size)
		mtimS, mtimNs := sysFi.Mtim.Unix()
		db.Mtime = time.Unix(mtimS, mtimNs)
		ctimS, ctimNs := sysFi.Ctim.Unix()
		db.Ctime = time.Unix(ctimS, ctimNs)
		ok = true
	} else {
		logger.Noticef("cannot stat state %v writing: %v", when, err)
	}
	return db, ok
}

func (osb *overlordStateBackend) Checkpoint(data []byte) error {
	before, _ := grabDebugStat(osb.path, "before")
	logger.Noticef("writing state (size %v)", len(data))

	defer func() {
		after, ok := grabDebugStat(osb.path, "after")
		if ok {
			logger.Noticef("replaced state at inode: %v (size %v) with inode %v (size %v) mtime %v ctime %v",
				before.Ino, before.Size,
				after.Ino, after.Size, after.Mtime, after.Ctime)
		}
	}()
	return osutil.AtomicWriteFile(osb.path, data, 0600, 0)
}

func (osb *overlordStateBackend) EnsureBefore(d time.Duration) {
	osb.ensureBefore(d)
}

func (osb *overlordStateBackend) RequestRestart(t state.RestartType) {
	osb.requestRestart(t)
}
