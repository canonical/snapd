// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/snapcore/snapd/overlord/aspectstate"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

var api = []*Command{
	rootCmd,
	sysInfoCmd,
	loginCmd,
	logoutCmd,
	appIconCmd,
	findCmd,
	snapsCmd,
	snapCmd,
	snapFileCmd,
	snapDownloadCmd,
	snapConfCmd,
	interfacesCmd,
	assertsCmd,
	assertsFindManyCmd,
	stateChangeCmd,
	stateChangesCmd,
	createUserCmd,
	buyCmd,
	readyToBuyCmd,
	snapctlCmd,
	usersCmd,
	sectionsCmd,
	categoriesCmd,
	aliasesCmd,
	appsCmd,
	logsCmd,
	warningsCmd,
	debugPprofCmd,
	debugCmd,
	snapshotCmd,
	snapshotExportCmd,
	connectionsCmd,
	modelCmd,
	cohortsCmd,
	serialModelCmd,
	systemsCmd,
	systemsActionCmd,
	themesCmd,
	accessoriesChangeCmd,
	validationSetsListCmd,
	validationSetsCmd,
	routineConsoleConfStartCmd,
	systemRecoveryKeysCmd,
	quotaGroupsCmd,
	quotaGroupInfoCmd,
	aspectsCmd,
	noticesCmd,
	noticeCmd,
	requestsPromptsCmd,
	requestsPromptCmd,
	requestsRulesCmd,
	requestsRuleCmd,
}

const (
	polkitActionLogin               = "io.snapcraft.snapd.login"
	polkitActionManage              = "io.snapcraft.snapd.manage"
	polkitActionManageInterfaces    = "io.snapcraft.snapd.manage-interfaces"
	polkitActionManageConfiguration = "io.snapcraft.snapd.manage-configuration"
)

// userFromRequest extracts user information from request and return the respective user in state, if valid
// It requires the state to be locked
func userFromRequest(st *state.State, req *http.Request) (*auth.UserState, error) {
	// extract macaroons data from request
	header := req.Header.Get("Authorization")
	if header == "" {
		return nil, auth.ErrInvalidAuth
	}

	authorizationData := strings.SplitN(header, " ", 2)
	if len(authorizationData) != 2 || authorizationData[0] != "Macaroon" {
		return nil, fmt.Errorf("authorization header misses Macaroon prefix")
	}

	var macaroon string
	var discharges []string
	for _, field := range strutil.CommaSeparatedList(authorizationData[1]) {
		if strings.HasPrefix(field, `root="`) {
			macaroon = strings.TrimSuffix(field[6:], `"`)
		}
		if strings.HasPrefix(field, `discharge="`) {
			discharges = append(discharges, strings.TrimSuffix(field[11:], `"`))
		}
	}

	if macaroon == "" {
		return nil, fmt.Errorf("invalid authorization header")
	}

	user, err := auth.CheckMacaroon(st, macaroon, discharges)
	return user, err
}

var muxVars = mux.Vars

func storeFrom(d *Daemon) snapstate.StoreService {
	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()

	return snapstate.Store(st, nil)
}

var (
	snapstateInstall                        = snapstate.Install
	snapstateInstallPath                    = snapstate.InstallPath
	snapstateInstallPathMany                = snapstate.InstallPathMany
	snapstateInstallComponentPath           = snapstate.InstallComponentPath
	snapstateRefreshCandidates              = snapstate.RefreshCandidates
	snapstateTryPath                        = snapstate.TryPath
	snapstateUpdate                         = snapstate.Update
	snapstateUpdateMany                     = snapstate.UpdateMany
	snapstateInstallMany                    = snapstate.InstallMany
	snapstateRemoveMany                     = snapstate.RemoveMany
	snapstateResolveValSetsEnforcementError = snapstate.ResolveValidationSetsEnforcementError
	snapstateRevert                         = snapstate.Revert
	snapstateRevertToRevision               = snapstate.RevertToRevision
	snapstateSwitch                         = snapstate.Switch
	snapstateProceedWithRefresh             = snapstate.ProceedWithRefresh
	snapstateHoldRefreshesBySystem          = snapstate.HoldRefreshesBySystem
	snapstateLongestGatingHold              = snapstate.LongestGatingHold
	snapstateSystemHold                     = snapstate.SystemHold

	configstateConfigureInstalled = configstate.ConfigureInstalled

	assertstateRefreshSnapAssertions         = assertstate.RefreshSnapAssertions
	assertstateRestoreValidationSetsTracking = assertstate.RestoreValidationSetsTracking

	aspectstateGetAspect = aspectstate.GetAspect
	aspectstateSetAspect = aspectstate.SetAspect
)

func ensureStateSoonImpl(st *state.State) {
	st.EnsureBefore(0)
}

var ensureStateSoon = ensureStateSoonImpl

func newChange(st *state.State, kind, summary string, tsets []*state.TaskSet, snapNames []string) *state.Change {
	chg := st.NewChange(kind, summary)
	for _, ts := range tsets {
		chg.AddAll(ts)
	}
	if snapNames != nil {
		chg.Set("snap-names", snapNames)
	}
	return chg
}

func isTrue(form *Form, key string) bool {
	values := form.Values[key]
	if len(values) == 0 {
		return false
	}
	b, err := strconv.ParseBool(values[0])
	if err != nil {
		return false
	}

	return b
}
