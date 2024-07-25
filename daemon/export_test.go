// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2022 Canonical Ltd
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

package daemon

import (
	"context"
	"net/http"
	"os/user"
	"time"

	"github.com/gorilla/mux"

	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var (
	CreateQuotaValues = createQuotaValues
	ParseOptionalTime = parseOptionalTime
)

func APICommands() []*Command {
	return api
}

func NewAndAddRoutes() (*Daemon, error) {
	d, err := New()
	if err != nil {
		return nil, err
	}
	d.addRoutes()
	return d, nil
}

func NewWithOverlord(o *overlord.Overlord) *Daemon {
	d := &Daemon{overlord: o, state: o.State()}
	d.addRoutes()
	return d
}

func (d *Daemon) RouterMatch(req *http.Request, m *mux.RouteMatch) bool {
	return d.router.Match(req, m)
}

func (d *Daemon) Overlord() *overlord.Overlord {
	return d.overlord
}

func (d *Daemon) RequestedRestart() restart.RestartType {
	return d.requestedRestart
}

type Ucrednet = ucrednet

func MockUcrednetGet(mock func(remoteAddr string) (ucred *Ucrednet, err error)) (restore func()) {
	oldUcrednetGet := ucrednetGet
	ucrednetGet = mock
	return func() {
		ucrednetGet = oldUcrednetGet
	}
}

func MockEnsureStateSoon(mock func(*state.State)) (original func(*state.State), restore func()) {
	oldEnsureStateSoon := ensureStateSoon
	ensureStateSoon = mock
	return ensureStateSoonImpl, func() {
		ensureStateSoon = oldEnsureStateSoon
	}
}

func MockMuxVars(vars func(*http.Request) map[string]string) (restore func()) {
	old := muxVars
	muxVars = vars
	return func() {
		muxVars = old
	}
}

func MockShutdownTimeout(tm time.Duration) (restore func()) {
	old := shutdownTimeout
	shutdownTimeout = tm
	return func() {
		shutdownTimeout = old
	}
}

func MockUnsafeReadSnapInfo(mock func(string) (*snap.Info, error)) (restore func()) {
	oldUnsafeReadSnapInfo := unsafeReadSnapInfo
	unsafeReadSnapInfo = mock
	return func() {
		unsafeReadSnapInfo = oldUnsafeReadSnapInfo
	}
}

func MockReadComponentInfoFromCont(mock func(tempPath string, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error)) (restore func()) {
	oldUnsafeReadSnapInfo := readComponentInfoFromCont
	readComponentInfoFromCont = mock
	return func() {
		readComponentInfoFromCont = oldUnsafeReadSnapInfo
	}
}

func MockAssertstateRefreshSnapAssertions(mock func(*state.State, int, *assertstate.RefreshAssertionsOptions) error) (restore func()) {
	oldAssertstateRefreshSnapAssertions := assertstateRefreshSnapAssertions
	assertstateRefreshSnapAssertions = mock
	return func() {
		assertstateRefreshSnapAssertions = oldAssertstateRefreshSnapAssertions
	}
}

func MockAssertstateTryEnforceValidationSets(f func(st *state.State, validationSets []string, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error) (restore func()) {
	r := testutil.Backup(&assertstateTryEnforcedValidationSets)
	assertstateTryEnforcedValidationSets = f
	return r
}

func MockSnapstateInstallOne(mock func(context.Context, *state.State, snapstate.InstallGoal, snapstate.Options) (*snap.Info, *state.TaskSet, error)) (restore func()) {
	old := snapstateInstallOne
	snapstateInstallOne = mock
	return func() {
		snapstateInstallOne = old
	}
}

func MockSnapstateInstallWithGoal(mock func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error)) (restore func()) {
	old := snapstateInstallWithGoal
	snapstateInstallWithGoal = mock
	return func() {
		snapstateInstallWithGoal = old
	}
}

func MockSnapstateStoreInstallGoal(mock func(snaps ...snapstate.StoreSnap) snapstate.InstallGoal) (restore func()) {
	old := snapstateStoreInstallGoal
	snapstateStoreInstallGoal = mock
	return func() {
		snapstateStoreInstallGoal = old
	}
}

func MockSnapstateInstallPath(mock func(*state.State, *snap.SideInfo, string, string, string, snapstate.Flags, snapstate.PrereqTracker) (*state.TaskSet, *snap.Info, error)) (restore func()) {
	oldSnapstateInstallPath := snapstateInstallPath
	snapstateInstallPath = mock
	return func() {
		snapstateInstallPath = oldSnapstateInstallPath
	}
}

func MockSnapstateUpdate(mock func(*state.State, string, *snapstate.RevisionOptions, int, snapstate.Flags) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateUpdate := snapstateUpdate
	snapstateUpdate = mock
	return func() {
		snapstateUpdate = oldSnapstateUpdate
	}
}

func MockSnapstateTryPath(mock func(*state.State, string, string, snapstate.Flags) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateTryPath := snapstateTryPath
	snapstateTryPath = mock
	return func() {
		snapstateTryPath = oldSnapstateTryPath
	}
}

func MockSnapstateSwitch(mock func(*state.State, string, *snapstate.RevisionOptions) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateSwitch := snapstateSwitch
	snapstateSwitch = mock
	return func() {
		snapstateSwitch = oldSnapstateSwitch
	}
}

func MockSnapstateRevert(mock func(*state.State, string, snapstate.Flags, string) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateRevert := snapstateRevert
	snapstateRevert = mock
	return func() {
		snapstateRevert = oldSnapstateRevert
	}
}

func MockSnapstateRevertToRevision(mock func(*state.State, string, snap.Revision, snapstate.Flags, string) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateRevertToRevision := snapstateRevertToRevision
	snapstateRevertToRevision = mock
	return func() {
		snapstateRevertToRevision = oldSnapstateRevertToRevision
	}
}

func MockSnapstateUpdateMany(mock func(context.Context, *state.State, []string, []*snapstate.RevisionOptions, int, *snapstate.Flags) ([]string, []*state.TaskSet, error)) (restore func()) {
	oldSnapstateUpdateMany := snapstateUpdateMany
	snapstateUpdateMany = mock
	return func() {
		snapstateUpdateMany = oldSnapstateUpdateMany
	}
}

func MockSnapstateRemove(mock func(st *state.State, name string, revision snap.Revision, flags *snapstate.RemoveFlags) (*state.TaskSet, error)) (restore func()) {
	oldSnapstateRemove := snapstateRemove
	snapstateRemove = mock
	return func() {
		snapstateRemove = oldSnapstateRemove
	}
}

func MockSnapstateRemoveMany(mock func(*state.State, []string, *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error)) (restore func()) {
	oldSnapstateRemoveMany := snapstateRemoveMany
	snapstateRemoveMany = mock
	return func() {
		snapstateRemoveMany = oldSnapstateRemoveMany
	}
}

func MockSnapstateInstallPathMany(f func(context.Context, *state.State, []*snap.SideInfo, []string, int, *snapstate.Flags) ([]*state.TaskSet, error)) func() {
	old := snapstateInstallPathMany
	snapstateInstallPathMany = f
	return func() {
		snapstateInstallPathMany = old
	}
}

func MockSnapstateInstallComponentPath(f func(st *state.State, csi *snap.ComponentSideInfo, info *snap.Info, path string, flags snapstate.Flags) (*state.TaskSet, error)) func() {
	old := snapstateInstallComponentPath
	snapstateInstallComponentPath = f
	return func() {
		snapstateInstallComponentPath = old
	}
}

func MockSnapstateResolveValSetEnforcementError(f func(context.Context, *state.State, *snapasserts.ValidationSetsValidationError, map[string]int, int) ([]*state.TaskSet, []string, error)) func() {
	old := snapstateResolveValSetsEnforcementError
	snapstateResolveValSetsEnforcementError = f
	return func() {
		snapstateResolveValSetsEnforcementError = old
	}
}

func MockSnapstateMigrate(mock func(*state.State, []string) ([]*state.TaskSet, error)) (restore func()) {
	oldSnapstateMigrate := snapstateMigrateHome
	snapstateMigrateHome = mock
	return func() {
		snapstateMigrateHome = oldSnapstateMigrate
	}
}

func MockSnapstateProceedWithRefresh(f func(st *state.State, gatingSnap string, snaps []string) error) (restore func()) {
	old := snapstateProceedWithRefresh
	snapstateProceedWithRefresh = f
	return func() {
		snapstateProceedWithRefresh = old
	}
}

func MockSnapstateHoldRefreshesBySystem(f func(st *state.State, level snapstate.HoldLevel, time string, snaps []string) error) (restore func()) {
	old := snapstateHoldRefreshesBySystem
	snapstateHoldRefreshesBySystem = f
	return func() {
		snapstateHoldRefreshesBySystem = old
	}
}

func MockSnapstateRemoveComponents(mock func(st *state.State, snapName string, compName []string, opts snapstate.RemoveComponentsOpts) ([]*state.TaskSet, error)) (restore func()) {
	oldSnapstateRemoveComponents := snapstateRemoveComponents
	snapstateRemoveComponents = mock
	return func() {
		snapstateRemoveComponents = oldSnapstateRemoveComponents
	}
}

func MockConfigstateConfigureInstalled(f func(st *state.State, name string, patchValues map[string]interface{}, flags int) (*state.TaskSet, error)) (restore func()) {
	old := configstateConfigureInstalled
	configstateConfigureInstalled = f
	return func() {
		configstateConfigureInstalled = old
	}
}

func MockSystemHold(f func(st *state.State, name string) (time.Time, error)) (restore func()) {
	old := snapstateSystemHold
	snapstateSystemHold = f
	return func() {
		snapstateSystemHold = old
	}
}

func MockLongestGatingHold(f func(st *state.State, name string) (time.Time, error)) (restore func()) {
	old := snapstateLongestGatingHold
	snapstateLongestGatingHold = f
	return func() {
		snapstateLongestGatingHold = old
	}
}

func MockReboot(f func(boot.RebootAction, time.Duration, *boot.RebootInfo) error) func() {
	reboot = f
	return func() { reboot = boot.Reboot }
}

func MockSideloadSnapsInfo(sis []*snap.SideInfo) (restore func()) {
	r := testutil.Backup(&sideloadSnapsInfo)
	sideloadSnapsInfo = func(st *state.State, snapFiles []*uploadedSnap,
		flags sideloadFlags) (*sideloadedInfo, *apiError) {

		names := make([]string, len(snapFiles))
		sideInfos := make([]*snap.SideInfo, len(snapFiles))
		origPaths := make([]string, len(snapFiles))
		tmpPaths := make([]string, len(snapFiles))
		for i, snapFile := range snapFiles {
			sideInfos[i] = sis[i]
			names[i] = sis[i].RealName
			origPaths[i] = snapFile.filename
			tmpPaths[i] = snapFile.tmpPath
		}
		return &sideloadedInfo{sideInfos: sideInfos, names: names,
			origPaths: origPaths, tmpPaths: tmpPaths}, nil
	}
	return r
}

type (
	RespJSON        = respJSON
	FileResponse    = fileResponse
	APIError        = apiError
	ErrorResult     = errorResult
	SnapInstruction = snapInstruction
)

func (inst *snapInstruction) Dispatch() snapActionFunc {
	return inst.dispatch()
}

func (inst *snapInstruction) DispatchForMany() snapManyActionFunc {
	return inst.dispatchForMany()
}

func (inst *snapInstruction) SetUserID(userID int) {
	inst.userID = userID
}

func (inst *snapInstruction) ModeFlags() (snapstate.Flags, error) {
	return inst.modeFlags()
}

func (inst *snapInstruction) ErrToResponse(err error) *APIError {
	return inst.errToResponse(err)
}

var (
	UserFromRequest = userFromRequest
	IsTrue          = isTrue

	MakeErrorResponder = makeErrorResponder
	ErrToResponse      = errToResponse

	MaxReadBuflen = maxReadBuflen
)

func MockRegistrystateGetViaView(f func(_ *state.State, _, _, _ string, _ []string) (interface{}, error)) (restore func()) {
	old := registrystateGetViaView
	registrystateGetViaView = f
	return func() {
		registrystateGetViaView = old
	}
}

func MockRegistrystateSetViaView(f func(_ *state.State, _, _, _ string, _ map[string]interface{}) error) (restore func()) {
	old := registrystateSetViaView
	registrystateSetViaView = f
	return func() {
		registrystateSetViaView = old
	}
}

func MockRebootNoticeWait(d time.Duration) (restore func()) {
	restore = testutil.Backup(&rebootNoticeWait)
	rebootNoticeWait = d
	return restore
}

func MockSystemUserFromRequest(f func(r *http.Request) (*user.User, error)) (restore func()) {
	restore = testutil.Backup(&systemUserFromRequest)
	systemUserFromRequest = f
	return restore
}

func MockOsReadlink(f func(string) (string, error)) func() {
	old := osReadlink
	osReadlink = f
	return func() {
		osReadlink = old
	}
}

func MockNewStatusDecorator(f func(ctx context.Context, isGlobal bool, uid string) clientutil.StatusDecorator) (restore func()) {
	restore = testutil.Backup(&newStatusDecorator)
	newStatusDecorator = f
	return restore
}
