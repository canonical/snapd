// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2024 Canonical Ltd
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

import (
	"os/user"
	"time"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

var (
	UpdatePiConfig       = updatePiConfig
	SwitchHandlePowerKey = switchHandlePowerKey
	SwitchDisableService = switchDisableService
	UpdateKeyValueStream = updateKeyValueStream
	AddFSOnlyHandler     = addFSOnlyHandler
	FilesystemOnlyApply  = filesystemOnlyApply
	UpdateHomedirsConfig = updateHomedirsConfig

	DoExperimentalApparmorPromptingDaemonRestart = doExperimentalApparmorPromptingDaemonRestart
)

type PlainCoreConfig = plainCoreConfig
type RepairConfig = repairConfig

// FilesystemOnlyRun is used for tests that run also when nomanagers flag is
// set, that is, for config groups that do not need access to the
// state but only the filesystem.
func FilesystemOnlyRun(dev sysconfig.Device, cfg ConfGetter) error {
	return filesystemOnlyRun(dev, cfg, nil)
}

func MockFindGid(f func(string) (uint64, error)) func() {
	return testutil.Mock(&osutilFindGid, f)
}

func MockChownPath(f func(string, sys.UserID, sys.GroupID) error) func() {
	return testutil.Mock(&sysChownPath, f)
}

func MockEnsureFileState(f func(string, osutil.FileState) error) func() {
	return testutil.Mock(&osutilEnsureFileState, f)
}

func MockDirExists(f func(string) (bool, bool, error)) func() {
	return testutil.Mock(&osutilDirExists, f)
}

func MockApparmorUpdateHomedirsTunable(f func([]string) error) func() {
	return testutil.Mock(&apparmorUpdateHomedirsTunable, f)
}

func MockApparmorSetupSnapConfineSnippets(f func() (bool, error)) func() {
	return testutil.Mock(&apparmorSetupSnapConfineSnippets, f)
}

func MockApparmorReloadAllSnapProfiles(f func() error) func() {
	return testutil.Mock(&apparmorReloadAllSnapProfiles, f)
}

func MockLoggerSimpleSetup(f func(opts *logger.LoggerOptions) error) func() {
	return testutil.Mock(&loggerSimpleSetup, f)
}

func MockRestartRequest(f func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo)) func() {
	return testutil.Mock(&restartRequest, f)
}

func MockServicestateControl(f func(st *state.State, appInfos []*snap.AppInfo, inst *servicestate.Instruction, cu *user.User, flags *servicestate.Flags, context *hookstate.Context) ([]*state.TaskSet, error)) func() {
	return testutil.Mock(&servicestateControl, f)
}

func MockServicestateChangeTimeout(v time.Duration) func() {
	return testutil.Mock(&serviceStartChangeTimeout, v)
}
