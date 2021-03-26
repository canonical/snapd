// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package configcore

import "github.com/snapcore/snapd/osutil/sys"

var (
	UpdatePiConfig                = updatePiConfig
	SwitchHandlePowerKey          = switchHandlePowerKey
	SwitchDisableService          = switchDisableService
	UpdateKeyValueStream          = updateKeyValueStream
	AddFSOnlyHandler              = addFSOnlyHandler
	AddWithStateHandler           = addWithStateHandler
	FilesystemOnlyApply           = filesystemOnlyApply
	PreinstallFilesystemOnlyApply = preinstallFilesystemOnlyApply
)

type PlainCoreConfig = plainCoreConfig

func MockFindGid(f func(string) (uint64, error)) func() {
	old := osutilFindGid
	osutilFindGid = f
	return func() {
		osutilFindGid = old
	}
}

func MockChownPath(f func(string, sys.UserID, sys.GroupID) error) func() {
	old := sysChownPath
	sysChownPath = f
	return func() {
		sysChownPath = old
	}
}
