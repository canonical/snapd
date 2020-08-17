// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
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
	aliasesCmd,
	appsCmd,
	logsCmd,
	warningsCmd,
	debugPprofCmd,
	debugCmd,
	snapshotCmd,
	connectionsCmd,
	modelCmd,
	cohortsCmd,
	serialModelCmd,
	systemsCmd,
	systemsActionCmd,
}

var servicestateControl = servicestate.Control

var (
	// see daemon.go:canAccess for details how the access is controlled
	rootCmd = &Command{
		Path:    "/",
		GuestOK: true,
		GET:     tbd,
	}

	sysInfoCmd = &Command{
		Path:    "/v2/system-info",
		GuestOK: true,
		GET:     sysInfo,
	}

	appIconCmd = &Command{
		Path:   "/v2/icons/{name}/icon",
		UserOK: true,
		GET:    appIconGet,
	}

	findCmd = &Command{
		Path:   "/v2/find",
		UserOK: true,
		GET:    searchStore,
	}

	snapsCmd = &Command{
		Path:     "/v2/snaps",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getSnapsInfo,
		POST:     postSnaps,
	}

	snapCmd = &Command{
		Path:     "/v2/snaps/{name}",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getSnapInfo,
		POST:     postSnap,
	}

	appsCmd = &Command{
		Path:   "/v2/apps",
		UserOK: true,
		GET:    getAppsInfo,
		POST:   postApps,
	}

	logsCmd = &Command{
		Path:     "/v2/logs",
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getLogs,
	}

	snapConfCmd = &Command{
		Path: "/v2/snaps/{name}/conf",
		GET:  getSnapConf,
		PUT:  setSnapConf,
	}

	interfacesCmd = &Command{
		Path:     "/v2/interfaces",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage-interfaces",
		GET:      interfacesConnectionsMultiplexer,
		POST:     changeInterfaces,
	}

	stateChangeCmd = &Command{
		Path:     "/v2/changes/{id}",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getChange,
		POST:     abortChange,
	}

	stateChangesCmd = &Command{
		Path:   "/v2/changes",
		UserOK: true,
		GET:    getChanges,
	}

	buyCmd = &Command{
		Path: "/v2/buy",
		POST: postBuy,
	}

	readyToBuyCmd = &Command{
		Path: "/v2/buy/ready",
		GET:  readyToBuy,
	}

	snapctlCmd = &Command{
		Path:   "/v2/snapctl",
		SnapOK: true,
		POST:   runSnapctl,
	}

	sectionsCmd = &Command{
		Path:   "/v2/sections",
		UserOK: true,
		GET:    getSections,
	}

	aliasesCmd = &Command{
		Path:   "/v2/aliases",
		UserOK: true,
		GET:    getAliases,
		POST:   changeAliases,
	}

	warningsCmd = &Command{
		Path:     "/v2/warnings",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getWarnings,
		POST:     ackWarnings,
	}

	buildID = "unknown"
)

var systemdVirt = ""

func init() {
	// cache the build-id on startup to ensure that changes in
	// the underlying binary do not affect us
	if bid, err := osutil.MyBuildID(); err == nil {
		buildID = bid
	}
	// cache systemd-detect-virt output as it's unlikely to change :-)
	if buf, err := exec.Command("systemd-detect-virt").CombinedOutput(); err == nil {
		systemdVirt = string(bytes.TrimSpace(buf))
	}
}

func tbd(c *Command, r *http.Request, user *auth.UserState) Response {
	return SyncResponse([]string{"TBD"}, nil)
}

func formatRefreshTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s", t.Truncate(time.Minute).Format(time.RFC3339))
}

func sysInfo(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	snapMgr := c.d.overlord.SnapManager()
	deviceMgr := c.d.overlord.DeviceManager()
	st.Lock()
	defer st.Unlock()
	nextRefresh := snapMgr.NextRefresh()
	lastRefresh, _ := snapMgr.LastRefresh()
	refreshHold, _ := snapMgr.EffectiveRefreshHold()
	refreshScheduleStr, legacySchedule, err := snapMgr.RefreshSchedule()
	if err != nil {
		return InternalError("cannot get refresh schedule: %s", err)
	}
	users, err := auth.Users(st)
	if err != nil && err != state.ErrNoState {
		return InternalError("cannot get user auth data: %s", err)
	}

	refreshInfo := client.RefreshInfo{
		Last: formatRefreshTime(lastRefresh),
		Hold: formatRefreshTime(refreshHold),
		Next: formatRefreshTime(nextRefresh),
	}
	if !legacySchedule {
		refreshInfo.Timer = refreshScheduleStr
	} else {
		refreshInfo.Schedule = refreshScheduleStr
	}

	m := map[string]interface{}{
		"series":         release.Series,
		"version":        c.d.Version,
		"build-id":       buildID,
		"os-release":     release.ReleaseInfo,
		"on-classic":     release.OnClassic,
		"managed":        len(users) > 0,
		"kernel-version": osutil.KernelVersion(),
		"locations": map[string]interface{}{
			"snap-mount-dir": dirs.SnapMountDir,
			"snap-bin-dir":   dirs.SnapBinariesDir,
		},
		"refresh":      refreshInfo,
		"architecture": arch.DpkgArchitecture(),
		"system-mode":  deviceMgr.SystemMode(),
	}
	if systemdVirt != "" {
		m["virtualization"] = systemdVirt
	}

	// NOTE: Right now we don't have a good way to differentiate if we
	// only have partial confinement (ala AppArmor disabled and Seccomp
	// enabled) or no confinement at all. Once we have a better system
	// in place how we can dynamically retrieve these information from
	// snapd we will use this here.
	if sandbox.ForceDevMode() {
		m["confinement"] = "partial"
	} else {
		m["confinement"] = "strict"
	}

	// Convey richer information about features of available security backends.
	if features := sandboxFeatures(c.d.overlord.InterfaceManager().Repository().Backends()); features != nil {
		m["sandbox-features"] = features
	}

	return SyncResponse(m, nil)
}

func sandboxFeatures(backends []interfaces.SecurityBackend) map[string][]string {
	result := make(map[string][]string, len(backends)+1)
	for _, backend := range backends {
		features := backend.SandboxFeatures()
		if len(features) > 0 {
			sort.Strings(features)
			result[string(backend.Name())] = features
		}
	}

	// Add information about supported confinement types as a fake backend
	features := make([]string, 1, 3)
	features[0] = "devmode"
	if !sandbox.ForceDevMode() {
		features = append(features, "strict")
	}
	if dirs.SupportsClassicConfinement() {
		features = append(features, "classic")
	}
	sort.Strings(features)
	result["confinement-options"] = features

	return result
}

// UserFromRequest extracts user information from request and return the respective user in state, if valid
// It requires the state to be locked
func UserFromRequest(st *state.State, req *http.Request) (*auth.UserState, error) {
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

func getSnapInfo(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	about, err := localSnapInfo(c.d.overlord.State(), name)
	if err != nil {
		if err == errNoSnap {
			return SnapNotFound(name, err)
		}

		return InternalError("%v", err)
	}

	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError("cannot find route for %q snap", name)
	}

	url, err := route.URL("name", name)
	if err != nil {
		return InternalError("cannot build URL for %q snap: %v", name, err)
	}

	sd := servicestate.NewStatusDecorator(progress.Null)

	result := webify(mapLocal(about, sd), url.String())

	return SyncResponse(result, nil)
}

func webify(result *client.Snap, resource string) *client.Snap {
	if result.Icon == "" || strings.HasPrefix(result.Icon, "http") {
		return result
	}
	result.Icon = ""

	route := appIconCmd.d.router.Get(appIconCmd.Path)
	if route != nil {
		url, err := route.URL("name", result.Name)
		if err == nil {
			result.Icon = url.String()
		}
	}

	return result
}

func getStore(c *Command) snapstate.StoreService {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	return snapstate.Store(st, nil)
}

func getSections(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	theStore := getStore(c)

	// TODO: use a per-request context
	sections, err := theStore.Sections(context.TODO(), user)
	switch err {
	case nil:
		// pass
	case store.ErrBadQuery:
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Result: &errorResult{Message: err.Error(), Kind: client.ErrorKindBadQuery},
			Status: 400,
		}, nil)
	case store.ErrUnauthenticated, store.ErrInvalidCredentials:
		return Unauthorized("%v", err)
	default:
		return InternalError("%v", err)
	}

	return SyncResponse(sections, nil)
}

func searchStore(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}
	query := r.URL.Query()
	q := query.Get("q")
	commonID := query.Get("common-id")
	// TODO: support both "category" (search v2) and "section"
	section := query.Get("section")
	name := query.Get("name")
	scope := query.Get("scope")
	private := false
	prefix := false

	if sel := query.Get("select"); sel != "" {
		switch sel {
		case "refresh":
			if commonID != "" {
				return BadRequest("cannot use 'common-id' with 'select=refresh'")
			}
			if name != "" {
				return BadRequest("cannot use 'name' with 'select=refresh'")
			}
			if q != "" {
				return BadRequest("cannot use 'q' with 'select=refresh'")
			}
			return storeUpdates(c, r, user)
		case "private":
			private = true
		}
	}

	if name != "" {
		if q != "" {
			return BadRequest("cannot use 'q' and 'name' together")
		}
		if commonID != "" {
			return BadRequest("cannot use 'common-id' and 'name' together")
		}

		if name[len(name)-1] != '*' {
			return findOne(c, r, user, name)
		}

		prefix = true
		q = name[:len(name)-1]
	}

	if commonID != "" && q != "" {
		return BadRequest("cannot use 'common-id' and 'q' together")
	}

	theStore := getStore(c)
	ctx := store.WithClientUserAgent(r.Context(), r)
	found, err := theStore.Find(ctx, &store.Search{
		Query:    q,
		Prefix:   prefix,
		CommonID: commonID,
		Category: section,
		Private:  private,
		Scope:    scope,
	}, user)
	switch err {
	case nil:
		// pass
	case store.ErrBadQuery:
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Result: &errorResult{Message: err.Error(), Kind: client.ErrorKindBadQuery},
			Status: 400,
		}, nil)
	case store.ErrUnauthenticated, store.ErrInvalidCredentials:
		return Unauthorized(err.Error())
	default:
		if e, ok := err.(*url.Error); ok {
			if neterr, ok := e.Err.(*net.OpError); ok {
				if dnserr, ok := neterr.Err.(*net.DNSError); ok {
					return SyncResponse(&resp{
						Type:   ResponseTypeError,
						Result: &errorResult{Message: dnserr.Error(), Kind: client.ErrorKindDNSFailure},
						Status: 400,
					}, nil)
				}
			}
		}
		if e, ok := err.(net.Error); ok && e.Timeout() {
			return SyncResponse(&resp{
				Type:   ResponseTypeError,
				Result: &errorResult{Message: err.Error(), Kind: client.ErrorKindNetworkTimeout},
				Status: 400,
			}, nil)
		}
		if e, ok := err.(*httputil.PerstistentNetworkError); ok {
			return SyncResponse(&resp{
				Type:   ResponseTypeError,
				Result: &errorResult{Message: e.Error(), Kind: client.ErrorKindDNSFailure},
				Status: 400,
			}, nil)
		}

		return InternalError("%v", err)
	}

	meta := &Meta{
		SuggestedCurrency: theStore.SuggestedCurrency(),
		Sources:           []string{"store"},
	}

	return sendStorePackages(route, meta, found)
}

func findOne(c *Command, r *http.Request, user *auth.UserState, name string) Response {
	if err := snap.ValidateName(name); err != nil {
		return BadRequest(err.Error())
	}

	theStore := getStore(c)
	spec := store.SnapSpec{
		Name: name,
	}
	ctx := store.WithClientUserAgent(r.Context(), r)
	snapInfo, err := theStore.SnapInfo(ctx, spec, user)
	switch err {
	case nil:
		// pass
	case store.ErrInvalidCredentials:
		return Unauthorized("%v", err)
	case store.ErrSnapNotFound:
		return SnapNotFound(name, err)
	default:
		return InternalError("%v", err)
	}

	meta := &Meta{
		SuggestedCurrency: theStore.SuggestedCurrency(),
		Sources:           []string{"store"},
	}

	results := make([]*json.RawMessage, 1)
	data, err := json.Marshal(webify(mapRemote(snapInfo), r.URL.String()))
	if err != nil {
		return InternalError(err.Error())
	}
	results[0] = (*json.RawMessage)(&data)
	return SyncResponse(results, meta)
}

func shouldSearchStore(r *http.Request) bool {
	// we should jump to the old behaviour iff q is given, or if
	// sources is given and either empty or contains the word
	// 'store'.  Otherwise, local results only.

	query := r.URL.Query()

	if _, ok := query["q"]; ok {
		logger.Debugf("use of obsolete \"q\" parameter: %q", r.URL)
		return true
	}

	if src, ok := query["sources"]; ok {
		logger.Debugf("use of obsolete \"sources\" parameter: %q", r.URL)
		if len(src) == 0 || strings.Contains(src[0], "store") {
			return true
		}
	}

	return false
}

func storeUpdates(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	state := c.d.overlord.State()
	state.Lock()
	updates, err := snapstateRefreshCandidates(state, user)
	state.Unlock()
	if err != nil {
		return InternalError("cannot list updates: %v", err)
	}

	return sendStorePackages(route, nil, updates)
}

func sendStorePackages(route *mux.Route, meta *Meta, found []*snap.Info) Response {
	results := make([]*json.RawMessage, 0, len(found))
	for _, x := range found {
		url, err := route.URL("name", x.InstanceName())
		if err != nil {
			logger.Noticef("Cannot build URL for snap %q revision %s: %v", x.InstanceName(), x.Revision, err)
			continue
		}

		data, err := json.Marshal(webify(mapRemote(x), url.String()))
		if err != nil {
			return InternalError("%v", err)
		}
		raw := json.RawMessage(data)
		results = append(results, &raw)
	}

	return SyncResponse(results, meta)
}

// plural!
func getSnapsInfo(c *Command, r *http.Request, user *auth.UserState) Response {

	if shouldSearchStore(r) {
		logger.Noticef("Jumping to \"find\" to better support legacy request %q", r.URL)
		return searchStore(c, r, user)
	}

	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	query := r.URL.Query()
	var all bool
	sel := query.Get("select")
	switch sel {
	case "all":
		all = true
	case "enabled", "":
		all = false
	default:
		return BadRequest("invalid select parameter: %q", sel)
	}
	var wanted map[string]bool
	if ns := query.Get("snaps"); len(ns) > 0 {
		nsl := strutil.CommaSeparatedList(ns)
		wanted = make(map[string]bool, len(nsl))
		for _, name := range nsl {
			wanted[name] = true
		}
	}

	found, err := allLocalSnapInfos(c.d.overlord.State(), all, wanted)
	if err != nil {
		return InternalError("cannot list local snaps! %v", err)
	}

	results := make([]*json.RawMessage, len(found))

	sd := servicestate.NewStatusDecorator(progress.Null)
	for i, x := range found {
		name := x.info.InstanceName()
		rev := x.info.Revision

		url, err := route.URL("name", name)
		if err != nil {
			logger.Noticef("Cannot build URL for snap %q revision %s: %v", name, rev, err)
			continue
		}

		data, err := json.Marshal(webify(mapLocal(x, sd), url.String()))
		if err != nil {
			return InternalError("cannot serialize snap %q revision %s: %v", name, rev, err)
		}
		raw := json.RawMessage(data)
		results[i] = &raw
	}

	return SyncResponse(results, &Meta{Sources: []string{"local"}})
}

// licenseData holds details about the snap license, and may be
// marshaled back as an error when the license agreement is pending,
// and is expected as input to accept (or not) that license
// agreement. As such, its field names are part of the API.
type licenseData struct {
	Intro   string `json:"intro"`
	License string `json:"license"`
	Agreed  bool   `json:"agreed"`
}

func (*licenseData) Error() string {
	return "license agreement required"
}

type snapRevisionOptions struct {
	Channel  string        `json:"channel"`
	Revision snap.Revision `json:"revision"`

	CohortKey   string `json:"cohort-key"`
	LeaveCohort bool   `json:"leave-cohort"`
}

func (ropt *snapRevisionOptions) validate() error {
	if ropt.CohortKey != "" {
		if ropt.LeaveCohort {
			return fmt.Errorf("cannot specify both cohort-key and leave-cohort")
		}
		if !ropt.Revision.Unset() {
			return fmt.Errorf("cannot specify both cohort-key and revision")
		}
	}

	if ropt.Channel != "" {
		_, err := channel.Parse(ropt.Channel, "-")
		if err != nil {
			return err
		}
	}
	return nil
}

type snapInstruction struct {
	progress.NullMeter

	Action string `json:"action"`
	Amend  bool   `json:"amend"`
	snapRevisionOptions
	DevMode          bool `json:"devmode"`
	JailMode         bool `json:"jailmode"`
	Classic          bool `json:"classic"`
	IgnoreValidation bool `json:"ignore-validation"`
	Unaliased        bool `json:"unaliased"`
	Purge            bool `json:"purge,omitempty"`
	// dropping support temporarely until flag confusion is sorted,
	// this isn't supported by client atm anyway
	LeaveOld bool         `json:"temp-dropped-leave-old"`
	License  *licenseData `json:"license"`
	Snaps    []string     `json:"snaps"`
	Users    []string     `json:"users"`

	// The fields below should not be unmarshalled into. Do not export them.
	userID int
	ctx    context.Context
}

func (inst *snapInstruction) revnoOpts() *snapstate.RevisionOptions {
	return &snapstate.RevisionOptions{
		Channel:     inst.Channel,
		Revision:    inst.Revision,
		CohortKey:   inst.CohortKey,
		LeaveCohort: inst.LeaveCohort,
	}
}

func (inst *snapInstruction) modeFlags() (snapstate.Flags, error) {
	return modeFlags(inst.DevMode, inst.JailMode, inst.Classic)
}

func (inst *snapInstruction) installFlags() (snapstate.Flags, error) {
	flags, err := inst.modeFlags()
	if err != nil {
		return snapstate.Flags{}, err
	}
	if inst.Unaliased {
		flags.Unaliased = true
	}
	return flags, nil
}

func (inst *snapInstruction) validate() error {
	if inst.CohortKey != "" {
		if inst.Action != "install" && inst.Action != "refresh" && inst.Action != "switch" {
			return fmt.Errorf("cohort-key can only be specified for install, refresh, or switch")
		}
	}
	if inst.LeaveCohort {
		if inst.Action != "refresh" && inst.Action != "switch" {
			return fmt.Errorf("leave-cohort can only be specified for refresh or switch")
		}
	}
	if inst.Action == "install" {
		for _, snapName := range inst.Snaps {
			// FIXME: alternatively we could simply mutate *inst
			//        and s/ubuntu-core/core/ ?
			if snapName == "ubuntu-core" {
				return fmt.Errorf(`cannot install "ubuntu-core", please use "core" instead`)
			}
		}
	}

	return inst.snapRevisionOptions.validate()
}

type snapInstructionResult struct {
	Summary  string
	Affected []string
	Tasksets []*state.TaskSet
	Result   map[string]interface{}
}

var (
	snapstateInstall           = snapstate.Install
	snapstateInstallPath       = snapstate.InstallPath
	snapstateRefreshCandidates = snapstate.RefreshCandidates
	snapstateTryPath           = snapstate.TryPath
	snapstateUpdate            = snapstate.Update
	snapstateUpdateMany        = snapstate.UpdateMany
	snapstateInstallMany       = snapstate.InstallMany
	snapstateRemoveMany        = snapstate.RemoveMany
	snapstateRevert            = snapstate.Revert
	snapstateRevertToRevision  = snapstate.RevertToRevision
	snapstateSwitch            = snapstate.Switch

	snapshotList    = snapshotstate.List
	snapshotCheck   = snapshotstate.Check
	snapshotForget  = snapshotstate.Forget
	snapshotRestore = snapshotstate.Restore
	snapshotSave    = snapshotstate.Save

	assertstateRefreshSnapDeclarations = assertstate.RefreshSnapDeclarations
)

func ensureStateSoonImpl(st *state.State) {
	st.EnsureBefore(0)
}

var ensureStateSoon = ensureStateSoonImpl

var errDevJailModeConflict = errors.New("cannot use devmode and jailmode flags together")
var errClassicDevmodeConflict = errors.New("cannot use classic and devmode flags together")
var errNoJailMode = errors.New("this system cannot honour the jailmode flag")

func modeFlags(devMode, jailMode, classic bool) (snapstate.Flags, error) {
	flags := snapstate.Flags{}
	devModeOS := sandbox.ForceDevMode()
	switch {
	case jailMode && devModeOS:
		return flags, errNoJailMode
	case jailMode && devMode:
		return flags, errDevJailModeConflict
	case devMode && classic:
		return flags, errClassicDevmodeConflict
	}
	// NOTE: jailmode and classic are allowed together. In that setting,
	// jailmode overrides classic and the app gets regular (non-classic)
	// confinement.
	flags.JailMode = jailMode
	flags.Classic = classic
	flags.DevMode = devMode
	return flags, nil
}

func snapUpdateMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	// we need refreshed snap-declarations to enforce refresh-control as best as we can, this also ensures that snap-declarations and their prerequisite assertions are updated regularly
	if err := assertstateRefreshSnapDeclarations(st, inst.userID); err != nil {
		return nil, err
	}

	// TODO: use a per-request context
	updated, tasksets, err := snapstateUpdateMany(context.TODO(), st, inst.Snaps, inst.userID, nil)
	if err != nil {
		return nil, err
	}

	var msg string
	switch len(updated) {
	case 0:
		if len(inst.Snaps) != 0 {
			// TRANSLATORS: the %s is a comma-separated list of quoted snap names
			msg = fmt.Sprintf(i18n.G("Refresh snaps %s: no updates"), strutil.Quoted(inst.Snaps))
		} else {
			msg = i18n.G("Refresh all snaps: no updates")
		}
	case 1:
		msg = fmt.Sprintf(i18n.G("Refresh snap %q"), updated[0])
	default:
		quoted := strutil.Quoted(updated)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Refresh snaps %s"), quoted)
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: updated,
		Tasksets: tasksets,
	}, nil
}

func snapInstallMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	for _, name := range inst.Snaps {
		if len(name) == 0 {
			return nil, fmt.Errorf(i18n.G("cannot install snap with empty name"))
		}
	}
	installed, tasksets, err := snapstateInstallMany(st, inst.Snaps, inst.userID)
	if err != nil {
		return nil, err
	}

	var msg string
	switch len(inst.Snaps) {
	case 0:
		return nil, fmt.Errorf("cannot install zero snaps")
	case 1:
		msg = fmt.Sprintf(i18n.G("Install snap %q"), inst.Snaps[0])
	default:
		quoted := strutil.Quoted(inst.Snaps)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Install snaps %s"), quoted)
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: installed,
		Tasksets: tasksets,
	}, nil
}

func snapInstall(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if len(inst.Snaps[0]) == 0 {
		return "", nil, fmt.Errorf(i18n.G("cannot install snap with empty name"))
	}

	flags, err := inst.installFlags()
	if err != nil {
		return "", nil, err
	}

	var ckey string
	if inst.CohortKey == "" {
		logger.Noticef("Installing snap %q revision %s", inst.Snaps[0], inst.Revision)
	} else {
		ckey = strutil.ElliptLeft(inst.CohortKey, 10)
		logger.Noticef("Installing snap %q from cohort %q", inst.Snaps[0], ckey)
	}
	tset, err := snapstateInstall(inst.ctx, st, inst.Snaps[0], inst.revnoOpts(), inst.userID, flags)
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap"), inst.Snaps[0])
	if inst.Channel != "stable" && inst.Channel != "" {
		msg += fmt.Sprintf(" from %q channel", inst.Channel)
	}
	if inst.CohortKey != "" {
		msg += fmt.Sprintf(" from %q cohort", ckey)
	}
	return msg, []*state.TaskSet{tset}, nil
}

func snapUpdate(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	// TODO: bail if revision is given (and != current?), *or* behave as with install --revision?
	flags, err := inst.modeFlags()
	if err != nil {
		return "", nil, err
	}
	if inst.IgnoreValidation {
		flags.IgnoreValidation = true
	}
	if inst.Amend {
		flags.Amend = true
	}

	// we need refreshed snap-declarations to enforce refresh-control as best as we can
	if err = assertstateRefreshSnapDeclarations(st, inst.userID); err != nil {
		return "", nil, err
	}

	ts, err := snapstateUpdate(st, inst.Snaps[0], inst.revnoOpts(), inst.userID, flags)
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Refresh %q snap"), inst.Snaps[0])
	if inst.Channel != "stable" && inst.Channel != "" {
		msg = fmt.Sprintf(i18n.G("Refresh %q snap from %q channel"), inst.Snaps[0], inst.Channel)
	}

	return msg, []*state.TaskSet{ts}, nil
}

func snapRemoveMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	removed, tasksets, err := snapstateRemoveMany(st, inst.Snaps)
	if err != nil {
		return nil, err
	}

	var msg string
	switch len(inst.Snaps) {
	case 0:
		return nil, fmt.Errorf("cannot remove zero snaps")
	case 1:
		msg = fmt.Sprintf(i18n.G("Remove snap %q"), inst.Snaps[0])
	default:
		quoted := strutil.Quoted(inst.Snaps)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Remove snaps %s"), quoted)
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: removed,
		Tasksets: tasksets,
	}, nil
}

func snapRemove(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	ts, err := snapstate.Remove(st, inst.Snaps[0], inst.Revision, &snapstate.RemoveFlags{Purge: inst.Purge})
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Remove %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapRevert(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	var ts *state.TaskSet

	flags, err := inst.modeFlags()
	if err != nil {
		return "", nil, err
	}

	if inst.Revision.Unset() {
		ts, err = snapstateRevert(st, inst.Snaps[0], flags)
	} else {
		ts, err = snapstateRevertToRevision(st, inst.Snaps[0], inst.Revision, flags)
	}
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Revert %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapEnable(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("enable takes no revision")
	}
	ts, err := snapstate.Enable(st, inst.Snaps[0])
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Enable %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapDisable(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("disable takes no revision")
	}
	ts, err := snapstate.Disable(st, inst.Snaps[0])
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Disable %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapSwitch(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("switch takes no revision")
	}
	ts, err := snapstateSwitch(st, inst.Snaps[0], inst.revnoOpts())
	if err != nil {
		return "", nil, err
	}

	var msg string
	switch {
	case inst.LeaveCohort && inst.Channel != "":
		msg = fmt.Sprintf(i18n.G("Switch %q snap to channel %q and away from cohort"), inst.Snaps[0], inst.Channel)
	case inst.LeaveCohort:
		msg = fmt.Sprintf(i18n.G("Switch %q snap away from cohort"), inst.Snaps[0])
	case inst.CohortKey == "" && inst.Channel != "":
		msg = fmt.Sprintf(i18n.G("Switch %q snap to channel %q"), inst.Snaps[0], inst.Channel)
	case inst.CohortKey != "" && inst.Channel == "":
		msg = fmt.Sprintf(i18n.G("Switch %q snap to cohort %q"), inst.Snaps[0], strutil.ElliptLeft(inst.CohortKey, 10))
	default:
		msg = fmt.Sprintf(i18n.G("Switch %q snap to channel %q and cohort %q"), inst.Snaps[0], inst.Channel, strutil.ElliptLeft(inst.CohortKey, 10))
	}
	return msg, []*state.TaskSet{ts}, nil
}

func snapshotMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	setID, snapshotted, ts, err := snapshotSave(st, inst.Snaps, inst.Users)
	if err != nil {
		return nil, err
	}

	var msg string
	if len(inst.Snaps) == 0 {
		msg = i18n.G("Snapshot all snaps")
	} else {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Snapshot snaps %s"), strutil.Quoted(inst.Snaps))
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: snapshotted,
		Tasksets: []*state.TaskSet{ts},
		Result:   map[string]interface{}{"set-id": setID},
	}, nil
}

type snapActionFunc func(*snapInstruction, *state.State) (string, []*state.TaskSet, error)

var snapInstructionDispTable = map[string]snapActionFunc{
	"install": snapInstall,
	"refresh": snapUpdate,
	"remove":  snapRemove,
	"revert":  snapRevert,
	"enable":  snapEnable,
	"disable": snapDisable,
	"switch":  snapSwitch,
}

func (inst *snapInstruction) dispatch() snapActionFunc {
	if len(inst.Snaps) != 1 {
		logger.Panicf("dispatch only handles single-snap ops; got %d", len(inst.Snaps))
	}
	return snapInstructionDispTable[inst.Action]
}

func (inst *snapInstruction) errToResponse(err error) Response {
	if len(inst.Snaps) == 0 {
		return errToResponse(err, nil, BadRequest, "cannot %s: %v", inst.Action)
	}

	return errToResponse(err, inst.Snaps, BadRequest, "cannot %s %s: %v", inst.Action, strutil.Quoted(inst.Snaps))
}

func postSnap(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	decoder := json.NewDecoder(r.Body)
	var inst snapInstruction
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("cannot decode request body into snap instruction: %v", err)
	}
	inst.ctx = r.Context()

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	vars := muxVars(r)
	inst.Snaps = []string{vars["name"]}

	if err := inst.validate(); err != nil {
		return BadRequest("%s", err)
	}

	impl := inst.dispatch()
	if impl == nil {
		return BadRequest("unknown action %s", inst.Action)
	}

	msg, tsets, err := impl(&inst, state)
	if err != nil {
		return inst.errToResponse(err)
	}

	chg := newChange(state, inst.Action+"-snap", msg, tsets, inst.Snaps)

	ensureStateSoon(state)

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

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

const maxReadBuflen = 1024 * 1024

func trySnap(c *Command, r *http.Request, user *auth.UserState, trydir string, flags snapstate.Flags) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if !filepath.IsAbs(trydir) {
		return BadRequest("cannot try %q: need an absolute path", trydir)
	}
	if !osutil.IsDirectory(trydir) {
		return BadRequest("cannot try %q: not a snap directory", trydir)
	}

	// the developer asked us to do this with a trusted snap dir
	info, err := unsafeReadSnapInfo(trydir)
	if _, ok := err.(snap.NotSnapError); ok {
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindNotSnap,
			},
			Status: 400,
		}, nil)
	}
	if err != nil {
		return BadRequest("cannot read snap info for %s: %s", trydir, err)
	}

	tset, err := snapstateTryPath(st, info.InstanceName(), trydir, flags)
	if err != nil {
		return errToResponse(err, []string{info.InstanceName()}, BadRequest, "cannot try %s: %s", trydir)
	}

	msg := fmt.Sprintf(i18n.G("Try %q snap from %s"), info.InstanceName(), trydir)
	chg := newChange(st, "try-snap", msg, []*state.TaskSet{tset}, []string{info.InstanceName()})
	chg.Set("api-data", map[string]string{"snap-name": info.InstanceName()})

	ensureStateSoon(st)

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func isTrue(form *multipart.Form, key string) bool {
	value := form.Value[key]
	if len(value) == 0 {
		return false
	}
	b, err := strconv.ParseBool(value[0])
	if err != nil {
		return false
	}

	return b
}

func snapsOp(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	decoder := json.NewDecoder(r.Body)
	var inst snapInstruction
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("cannot decode request body into snap instruction: %v", err)
	}

	// TODO: inst.Amend, etc?
	if inst.Channel != "" || !inst.Revision.Unset() || inst.DevMode || inst.JailMode || inst.CohortKey != "" || inst.LeaveCohort || inst.Purge {
		return BadRequest("unsupported option provided for multi-snap operation")
	}
	if err := inst.validate(); err != nil {
		return BadRequest("%v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	var op func(*snapInstruction, *state.State) (*snapInstructionResult, error)

	switch inst.Action {
	case "refresh":
		op = snapUpdateMany
	case "install":
		op = snapInstallMany
	case "remove":
		op = snapRemoveMany
	case "snapshot":
		op = snapshotMany
	default:
		return BadRequest("unsupported multi-snap operation %q", inst.Action)
	}
	res, err := op(&inst, st)
	if err != nil {
		return inst.errToResponse(err)
	}

	var chg *state.Change
	if len(res.Tasksets) == 0 {
		chg = st.NewChange(inst.Action+"-snap", res.Summary)
		chg.SetStatus(state.DoneStatus)
	} else {
		chg = newChange(st, inst.Action+"-snap", res.Summary, res.Tasksets, res.Affected)
		ensureStateSoon(st)
	}

	chg.Set("api-data", map[string]interface{}{"snap-names": res.Affected})

	return AsyncResponse(res.Result, &Meta{Change: chg.ID()})
}

func postSnaps(c *Command, r *http.Request, user *auth.UserState) Response {
	contentType := r.Header.Get("Content-Type")

	if contentType == "application/json" {
		return snapsOp(c, r, user)
	}

	if !strings.HasPrefix(contentType, "multipart/") {
		return BadRequest("unknown content type: %s", contentType)
	}

	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	// POSTs to sideload snaps must be a multipart/form-data file upload.
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return BadRequest("cannot parse POST body: %v", err)
	}

	form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(maxReadBuflen)
	if err != nil {
		return BadRequest("cannot read POST form: %v", err)
	}

	dangerousOK := isTrue(form, "dangerous")
	flags, err := modeFlags(isTrue(form, "devmode"), isTrue(form, "jailmode"), isTrue(form, "classic"))
	if err != nil {
		return BadRequest(err.Error())
	}

	if len(form.Value["action"]) > 0 && form.Value["action"][0] == "try" {
		if len(form.Value["snap-path"]) == 0 {
			return BadRequest("need 'snap-path' value in form")
		}
		return trySnap(c, r, user, form.Value["snap-path"][0], flags)
	}
	flags.RemoveSnapPath = true

	flags.Unaliased = isTrue(form, "unaliased")

	// find the file for the "snap" form field
	var snapBody multipart.File
	var origPath string
out:
	for name, fheaders := range form.File {
		if name != "snap" {
			continue
		}
		for _, fheader := range fheaders {
			snapBody, err = fheader.Open()
			origPath = fheader.Filename
			if err != nil {
				return BadRequest(`cannot open uploaded "snap" file: %v`, err)
			}
			defer snapBody.Close()

			break out
		}
	}
	defer form.RemoveAll()

	if snapBody == nil {
		return BadRequest(`cannot find "snap" file field in provided multipart/form-data payload`)
	}

	// we are in charge of the tempfile life cycle until we hand it off to the change
	changeTriggered := false
	// if you change this prefix, look for it in the tests
	// also see localInstallCleanup in snapstate/snapmgr.go
	tmpf, err := ioutil.TempFile(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix)
	if err != nil {
		return InternalError("cannot create temporary file: %v", err)
	}

	tempPath := tmpf.Name()

	defer func() {
		if !changeTriggered {
			os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(tmpf, snapBody); err != nil {
		return InternalError("cannot copy request into temporary file: %v", err)
	}
	tmpf.Sync()

	if len(form.Value["snap-path"]) > 0 {
		origPath = form.Value["snap-path"][0]
	}

	var instanceName string

	if len(form.Value["name"]) > 0 {
		// caller has specified desired instance name
		instanceName = form.Value["name"][0]
		if err := snap.ValidateInstanceName(instanceName); err != nil {
			return BadRequest(err.Error())
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var snapName string
	var sideInfo *snap.SideInfo

	if !dangerousOK {
		si, err := snapasserts.DeriveSideInfo(tempPath, assertstate.DB(st))
		switch {
		case err == nil:
			snapName = si.RealName
			sideInfo = si
		case asserts.IsNotFound(err):
			// with devmode we try to find assertions but it's ok
			// if they are not there (implies --dangerous)
			if !isTrue(form, "devmode") {
				msg := "cannot find signatures with metadata for snap"
				if origPath != "" {
					msg = fmt.Sprintf("%s %q", msg, origPath)
				}
				return BadRequest(msg)
			}
			// TODO: set a warning if devmode
		default:
			return BadRequest(err.Error())
		}
	}

	if snapName == "" {
		// potentially dangerous but dangerous or devmode params were set
		info, err := unsafeReadSnapInfo(tempPath)
		if err != nil {
			return BadRequest("cannot read snap file: %v", err)
		}
		snapName = info.SnapName()
		sideInfo = &snap.SideInfo{RealName: snapName}
	}

	if instanceName != "" {
		requestedSnapName := snap.InstanceSnap(instanceName)
		if requestedSnapName != snapName {
			return BadRequest(fmt.Sprintf("instance name %q does not match snap name %q", instanceName, snapName))
		}
	} else {
		instanceName = snapName
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap from file"), instanceName)
	if origPath != "" {
		msg = fmt.Sprintf(i18n.G("Install %q snap from file %q"), instanceName, origPath)
	}

	tset, _, err := snapstateInstallPath(st, sideInfo, tempPath, instanceName, "", flags)
	if err != nil {
		return errToResponse(err, []string{snapName}, InternalError, "cannot install snap file: %v")
	}

	chg := newChange(st, "install-snap", msg, []*state.TaskSet{tset}, []string{instanceName})
	chg.Set("api-data", map[string]string{"snap-name": instanceName})

	ensureStateSoon(st)

	// only when the unlock succeeds (as opposed to panicing) is the handoff done
	// but this is good enough
	changeTriggered = true

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func unsafeReadSnapInfoImpl(snapPath string) (*snap.Info, error) {
	// Condider using DeriveSideInfo before falling back to this!
	snapf, err := snapfile.Open(snapPath)
	if err != nil {
		return nil, err
	}
	return snap.ReadInfoFromSnapFile(snapf, nil)
}

var unsafeReadSnapInfo = unsafeReadSnapInfoImpl

func iconGet(st *state.State, name string) Response {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, name, &snapst)
	if err != nil {
		if err == state.ErrNoState {
			return SnapNotFound(name, err)
		}
		return InternalError("cannot consult state: %v", err)
	}
	sideInfo := snapst.CurrentSideInfo()
	if sideInfo == nil {
		return NotFound("snap has no current revision")
	}

	icon := snapIcon(snap.MinimalPlaceInfo(name, sideInfo.Revision))

	if icon == "" {
		return NotFound("local snap has no icon")
	}

	return fileResponse(icon)
}

func appIconGet(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	return iconGet(c.d.overlord.State(), name)
}

func getSnapConf(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	snapName := configstate.RemapSnapFromRequest(vars["name"])

	keys := strutil.CommaSeparatedList(r.URL.Query().Get("keys"))

	s := c.d.overlord.State()
	s.Lock()
	tr := config.NewTransaction(s)
	s.Unlock()
	// ensure configcore can hijack requests to e.g. hostname
	configcore.RegisterHijackers(tr)

	currentConfValues := make(map[string]interface{})
	// Special case - return root document
	if len(keys) == 0 {
		keys = []string{""}
	}
	for _, key := range keys {
		var value interface{}
		if err := tr.Get(snapName, key, &value); err != nil {
			if config.IsNoOption(err) {
				if key == "" {
					// no configuration - return empty document
					currentConfValues = make(map[string]interface{})
					break
				}
				return SyncResponse(&resp{
					Type: ResponseTypeError,
					Result: &errorResult{
						Message: err.Error(),
						Kind:    client.ErrorKindConfigNoSuchOption,
						Value:   err,
					},
					Status: 400,
				}, nil)
			} else {
				return InternalError("%v", err)
			}
		}
		if key == "" {
			if len(keys) > 1 {
				return BadRequest("keys contains zero-length string")
			}
			return SyncResponse(value, nil)
		}

		currentConfValues[key] = value
	}

	return SyncResponse(currentConfValues, nil)
}

func setSnapConf(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	snapName := configstate.RemapSnapFromRequest(vars["name"])

	var patchValues map[string]interface{}
	if err := jsonutil.DecodeWithNumber(r.Body, &patchValues); err != nil {
		return BadRequest("cannot decode request body into patch values: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	taskset, err := configstate.ConfigureInstalled(st, snapName, patchValues, 0)
	if err != nil {
		// TODO: just return snap-not-installed instead ?
		if _, ok := err.(*snap.NotInstalledError); ok {
			return SnapNotFound(snapName, err)
		}
		return errToResponse(err, []string{snapName}, InternalError, "%v")
	}

	summary := fmt.Sprintf("Change configuration of %q snap", snapName)
	change := newChange(st, "configure-snap", summary, []*state.TaskSet{taskset}, []string{snapName})

	st.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: change.ID()})
}

// interfacesConnectionsMultiplexer multiplexes to either legacy (connection) or modern behavior (interfaces).
func interfacesConnectionsMultiplexer(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	qselect := query.Get("select")
	if qselect == "" {
		return getLegacyConnections(c, r, user)
	} else {
		return getInterfaces(c, r, user)
	}
}

func getInterfaces(c *Command, r *http.Request, user *auth.UserState) Response {
	// Collect query options from request arguments.
	q := r.URL.Query()
	pselect := q.Get("select")
	if pselect != "all" && pselect != "connected" {
		return BadRequest("unsupported select qualifier")
	}
	var names []string // Interface names
	namesStr := q.Get("names")
	if namesStr != "" {
		names = strings.Split(namesStr, ",")
	}
	opts := &interfaces.InfoOptions{
		Names:     names,
		Doc:       q.Get("doc") == "true",
		Plugs:     q.Get("plugs") == "true",
		Slots:     q.Get("slots") == "true",
		Connected: pselect == "connected",
	}
	// Query the interface repository (this returns []*interface.Info).
	infos := c.d.overlord.InterfaceManager().Repository().Info(opts)
	infoJSONs := make([]*interfaceJSON, 0, len(infos))

	for _, info := range infos {
		// Convert interfaces.Info into interfaceJSON
		plugs := make([]*plugJSON, 0, len(info.Plugs))
		for _, plug := range info.Plugs {
			plugs = append(plugs, &plugJSON{
				Snap:  plug.Snap.InstanceName(),
				Name:  plug.Name,
				Attrs: plug.Attrs,
				Label: plug.Label,
			})
		}
		slots := make([]*slotJSON, 0, len(info.Slots))
		for _, slot := range info.Slots {
			slots = append(slots, &slotJSON{
				Snap:  slot.Snap.InstanceName(),
				Name:  slot.Name,
				Attrs: slot.Attrs,
				Label: slot.Label,
			})
		}
		infoJSONs = append(infoJSONs, &interfaceJSON{
			Name:    info.Name,
			Summary: info.Summary,
			DocURL:  info.DocURL,
			Plugs:   plugs,
			Slots:   slots,
		})
	}
	return SyncResponse(infoJSONs, nil)
}

func getLegacyConnections(c *Command, r *http.Request, user *auth.UserState) Response {
	connsjson, err := collectConnections(c.d.overlord.InterfaceManager(), collectFilter{})
	if err != nil {
		return InternalError("collecting connection information failed: %v", err)
	}
	legacyconnsjson := legacyConnectionsJSON{
		Plugs: connsjson.Plugs,
		Slots: connsjson.Slots,
	}
	return SyncResponse(legacyconnsjson, nil)
}

func snapNamesFromConns(conns []*interfaces.ConnRef) []string {
	m := make(map[string]bool)
	for _, conn := range conns {
		m[conn.PlugRef.Snap] = true
		m[conn.SlotRef.Snap] = true
	}
	l := make([]string, 0, len(m))
	for name := range m {
		l = append(l, name)
	}
	sort.Strings(l)
	return l
}

// changeInterfaces controls the interfaces system.
// Plugs can be connected to and disconnected from slots.
func changeInterfaces(c *Command, r *http.Request, user *auth.UserState) Response {
	var a interfaceAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&a); err != nil {
		return BadRequest("cannot decode request body into an interface action: %v", err)
	}
	if a.Action == "" {
		return BadRequest("interface action not specified")
	}
	if len(a.Plugs) > 1 || len(a.Slots) > 1 {
		return NotImplemented("many-to-many operations are not implemented")
	}
	if a.Action != "connect" && a.Action != "disconnect" {
		return BadRequest("unsupported interface action: %q", a.Action)
	}
	if len(a.Plugs) == 0 || len(a.Slots) == 0 {
		return BadRequest("at least one plug and slot is required")
	}

	var summary string
	var err error

	var tasksets []*state.TaskSet
	var affected []string

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	for i := range a.Plugs {
		a.Plugs[i].Snap = ifacestate.RemapSnapFromRequest(a.Plugs[i].Snap)
	}
	for i := range a.Slots {
		a.Slots[i].Snap = ifacestate.RemapSnapFromRequest(a.Slots[i].Snap)
	}

	switch a.Action {
	case "connect":
		var connRef *interfaces.ConnRef
		repo := c.d.overlord.InterfaceManager().Repository()
		connRef, err = repo.ResolveConnect(a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		if err == nil {
			var ts *state.TaskSet
			affected = snapNamesFromConns([]*interfaces.ConnRef{connRef})
			summary = fmt.Sprintf("Connect %s:%s to %s:%s", connRef.PlugRef.Snap, connRef.PlugRef.Name, connRef.SlotRef.Snap, connRef.SlotRef.Name)
			ts, err = ifacestate.Connect(st, connRef.PlugRef.Snap, connRef.PlugRef.Name, connRef.SlotRef.Snap, connRef.SlotRef.Name)
			if _, ok := err.(*ifacestate.ErrAlreadyConnected); ok {
				change := newChange(st, a.Action+"-snap", summary, nil, affected)
				change.SetStatus(state.DoneStatus)
				return AsyncResponse(nil, &Meta{Change: change.ID()})
			}
			tasksets = append(tasksets, ts)
		}
	case "disconnect":
		var conns []*interfaces.ConnRef
		summary = fmt.Sprintf("Disconnect %s:%s from %s:%s", a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		conns, err = c.d.overlord.InterfaceManager().ResolveDisconnect(a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name, a.Forget)
		if err == nil {
			if len(conns) == 0 {
				return InterfacesUnchanged("nothing to do")
			}
			repo := c.d.overlord.InterfaceManager().Repository()
			for _, connRef := range conns {
				var ts *state.TaskSet
				var conn *interfaces.Connection
				if a.Forget {
					ts, err = ifacestate.Forget(st, repo, connRef)
				} else {
					conn, err = repo.Connection(connRef)
					if err != nil {
						break
					}
					ts, err = ifacestate.Disconnect(st, conn)
					if err != nil {
						break
					}
				}
				if err != nil {
					break
				}
				ts.JoinLane(st.NewLane())
				tasksets = append(tasksets, ts)
			}
			affected = snapNamesFromConns(conns)
		}
	}
	if err != nil {
		return errToResponse(err, nil, BadRequest, "%v")
	}

	change := newChange(st, a.Action+"-snap", summary, tasksets, affected)
	st.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: change.ID()})
}

type changeInfo struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Summary string      `json:"summary"`
	Status  string      `json:"status"`
	Tasks   []*taskInfo `json:"tasks,omitempty"`
	Ready   bool        `json:"ready"`
	Err     string      `json:"err,omitempty"`

	SpawnTime time.Time  `json:"spawn-time,omitempty"`
	ReadyTime *time.Time `json:"ready-time,omitempty"`

	Data map[string]*json.RawMessage `json:"data,omitempty"`
}

type taskInfo struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Summary  string           `json:"summary"`
	Status   string           `json:"status"`
	Log      []string         `json:"log,omitempty"`
	Progress taskInfoProgress `json:"progress"`

	SpawnTime time.Time  `json:"spawn-time,omitempty"`
	ReadyTime *time.Time `json:"ready-time,omitempty"`
}

type taskInfoProgress struct {
	Label string `json:"label"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

func change2changeInfo(chg *state.Change) *changeInfo {
	status := chg.Status()
	chgInfo := &changeInfo{
		ID:      chg.ID(),
		Kind:    chg.Kind(),
		Summary: chg.Summary(),
		Status:  status.String(),
		Ready:   status.Ready(),

		SpawnTime: chg.SpawnTime(),
	}
	readyTime := chg.ReadyTime()
	if !readyTime.IsZero() {
		chgInfo.ReadyTime = &readyTime
	}
	if err := chg.Err(); err != nil {
		chgInfo.Err = err.Error()
	}

	tasks := chg.Tasks()
	taskInfos := make([]*taskInfo, len(tasks))
	for j, t := range tasks {
		label, done, total := t.Progress()

		taskInfo := &taskInfo{
			ID:      t.ID(),
			Kind:    t.Kind(),
			Summary: t.Summary(),
			Status:  t.Status().String(),
			Log:     t.Log(),
			Progress: taskInfoProgress{
				Label: label,
				Done:  done,
				Total: total,
			},
			SpawnTime: t.SpawnTime(),
		}
		readyTime := t.ReadyTime()
		if !readyTime.IsZero() {
			taskInfo.ReadyTime = &readyTime
		}
		taskInfos[j] = taskInfo
	}
	chgInfo.Tasks = taskInfos

	var data map[string]*json.RawMessage
	if chg.Get("api-data", &data) == nil {
		chgInfo.Data = data
	}

	return chgInfo
}

func getChange(c *Command, r *http.Request, user *auth.UserState) Response {
	chID := muxVars(r)["id"]
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chg := state.Change(chID)
	if chg == nil {
		return NotFound("cannot find change with id %q", chID)
	}

	return SyncResponse(change2changeInfo(chg), nil)
}

func getChanges(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	qselect := query.Get("select")
	if qselect == "" {
		qselect = "in-progress"
	}
	var filter func(*state.Change) bool
	switch qselect {
	case "all":
		filter = func(*state.Change) bool { return true }
	case "in-progress":
		filter = func(chg *state.Change) bool { return !chg.Status().Ready() }
	case "ready":
		filter = func(chg *state.Change) bool { return chg.Status().Ready() }
	default:
		return BadRequest("select should be one of: all,in-progress,ready")
	}

	if wantedName := query.Get("for"); wantedName != "" {
		outerFilter := filter
		filter = func(chg *state.Change) bool {
			if !outerFilter(chg) {
				return false
			}

			var snapNames []string
			if err := chg.Get("snap-names", &snapNames); err != nil {
				logger.Noticef("Cannot get snap-name for change %v", chg.ID())
				return false
			}

			for _, name := range snapNames {
				// due to
				// https://bugs.launchpad.net/snapd/+bug/1880560
				// the snap-names in service-control changes
				// could have included <snap>.<app>
				snapName, _ := snap.SplitSnapApp(name)
				if snapName == wantedName {
					return true
				}
			}
			return false
		}
	}

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chgs := state.Changes()
	chgInfos := make([]*changeInfo, 0, len(chgs))
	for _, chg := range chgs {
		if !filter(chg) {
			continue
		}
		chgInfos = append(chgInfos, change2changeInfo(chg))
	}
	return SyncResponse(chgInfos, nil)
}

func abortChange(c *Command, r *http.Request, user *auth.UserState) Response {
	chID := muxVars(r)["id"]
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chg := state.Change(chID)
	if chg == nil {
		return NotFound("cannot find change with id %q", chID)
	}

	var reqData struct {
		Action string `json:"action"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reqData); err != nil {
		return BadRequest("cannot decode data from request body: %v", err)
	}

	if reqData.Action != "abort" {
		return BadRequest("change action %q is unsupported", reqData.Action)
	}

	if chg.Status().Ready() {
		return BadRequest("cannot abort change %s with nothing pending", chID)
	}

	// flag the change
	chg.Abort()

	// actually ask to proceed with the abort
	ensureStateSoon(state)

	return SyncResponse(change2changeInfo(chg), nil)
}

var (
	runSnapctlUcrednetGet = ucrednetGet
	ctlcmdRun             = ctlcmd.Run
)

func convertBuyError(err error) Response {
	switch err {
	case nil:
		return nil
	case store.ErrInvalidCredentials:
		return Unauthorized(err.Error())
	case store.ErrUnauthenticated:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindLoginRequired,
			},
			Status: 400,
		}, nil)
	case store.ErrTOSNotAccepted:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindTermsNotAccepted,
			},
			Status: 400,
		}, nil)
	case store.ErrNoPaymentMethods:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindNoPaymentMethods,
			},
			Status: 400,
		}, nil)
	case store.ErrPaymentDeclined:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindPaymentDeclined,
			},
			Status: 400,
		}, nil)
	default:
		return InternalError("%v", err)
	}
}

func postBuy(c *Command, r *http.Request, user *auth.UserState) Response {
	var opts client.BuyOptions

	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&opts)
	if err != nil {
		return BadRequest("cannot decode buy options from request body: %v", err)
	}

	s := getStore(c)

	buyResult, err := s.Buy(&opts, user)

	if resp := convertBuyError(err); resp != nil {
		return resp
	}

	return SyncResponse(buyResult, nil)
}

func readyToBuy(c *Command, r *http.Request, user *auth.UserState) Response {
	s := getStore(c)

	if resp := convertBuyError(s.ReadyToBuy(user)); resp != nil {
		return resp
	}

	return SyncResponse(true, nil)
}

func runSnapctl(c *Command, r *http.Request, user *auth.UserState) Response {
	var snapctlOptions client.SnapCtlOptions
	if err := jsonutil.DecodeWithNumber(r.Body, &snapctlOptions); err != nil {
		return BadRequest("cannot decode snapctl request: %s", err)
	}

	if len(snapctlOptions.Args) == 0 {
		return BadRequest("snapctl cannot run without args")
	}

	_, uid, _, err := runSnapctlUcrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %s", err)
	}

	// Ignore missing context error to allow 'snapctl -h' without a context;
	// Actual context is validated later by get/set.
	context, _ := c.d.overlord.HookManager().Context(snapctlOptions.ContextID)
	stdout, stderr, err := ctlcmdRun(context, snapctlOptions.Args, uid)
	if err != nil {
		if e, ok := err.(*ctlcmd.UnsuccessfulError); ok {
			result := map[string]interface{}{
				"stdout":    string(stdout),
				"stderr":    string(stderr),
				"exit-code": e.ExitCode,
			}
			return &resp{
				Type: ResponseTypeError,
				Result: &errorResult{
					Message: e.Error(),
					Kind:    client.ErrorKindUnsuccessful,
					Value:   result,
				},
				Status: 200,
			}
		}
		if e, ok := err.(*ctlcmd.ForbiddenCommandError); ok {
			return Forbidden(e.Error())
		}
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			stdout = []byte(e.Error())
		} else {
			return BadRequest("error running snapctl: %s", err)
		}
	}

	if context != nil && context.IsEphemeral() {
		context.Lock()
		defer context.Unlock()
		if err := context.Done(); err != nil {
			return BadRequest(i18n.G("set failed: %v"), err)
		}
	}

	result := map[string]string{
		"stdout": string(stdout),
		"stderr": string(stderr),
	}

	return SyncResponse(result, nil)
}

// aliasAction is an action performed on aliases
type aliasAction struct {
	Action string `json:"action"`
	Snap   string `json:"snap"`
	App    string `json:"app"`
	Alias  string `json:"alias"`
	// old now unsupported api
	Aliases []string `json:"aliases"`
}

func changeAliases(c *Command, r *http.Request, user *auth.UserState) Response {
	var a aliasAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&a); err != nil {
		return BadRequest("cannot decode request body into an alias action: %v", err)
	}
	if len(a.Aliases) != 0 {
		return BadRequest("cannot interpret request, snaps can no longer be expected to declare their aliases")
	}

	var taskset *state.TaskSet
	var err error

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch a.Action {
	default:
		return BadRequest("unsupported alias action: %q", a.Action)
	case "alias":
		taskset, err = snapstate.Alias(st, a.Snap, a.App, a.Alias)
	case "unalias":
		if a.Alias == a.Snap {
			// Do What I mean:
			// check if a snap is referred/intended
			// or just an alias
			var snapst snapstate.SnapState
			err := snapstate.Get(st, a.Snap, &snapst)
			if err != nil && err != state.ErrNoState {
				return InternalError("%v", err)
			}
			if err == state.ErrNoState { // not a snap
				a.Snap = ""
			}
		}
		if a.Snap != "" {
			a.Alias = ""
			taskset, err = snapstate.DisableAllAliases(st, a.Snap)
		} else {
			taskset, a.Snap, err = snapstate.RemoveManualAlias(st, a.Alias)
		}
	case "prefer":
		taskset, err = snapstate.Prefer(st, a.Snap)
	}
	if err != nil {
		return errToResponse(err, nil, BadRequest, "%v")
	}

	var summary string
	switch a.Action {
	case "alias":
		summary = fmt.Sprintf(i18n.G("Setup alias %q => %q for snap %q"), a.Alias, a.App, a.Snap)
	case "unalias":
		if a.Alias != "" {
			summary = fmt.Sprintf(i18n.G("Remove manual alias %q for snap %q"), a.Alias, a.Snap)
		} else {
			summary = fmt.Sprintf(i18n.G("Disable all aliases for snap %q"), a.Snap)
		}
	case "prefer":
		summary = fmt.Sprintf(i18n.G("Prefer aliases of snap %q"), a.Snap)
	}

	change := newChange(st, a.Action, summary, []*state.TaskSet{taskset}, []string{a.Snap})
	st.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: change.ID()})
}

type aliasStatus struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Manual  string `json:"manual,omitempty"`
	Auto    string `json:"auto,omitempty"`
}

// getAliases produces a response with a map snap -> alias -> aliasStatus
func getAliases(c *Command, r *http.Request, user *auth.UserState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	res := make(map[string]map[string]aliasStatus)

	allStates, err := snapstate.All(state)
	if err != nil {
		return InternalError("cannot list local snaps: %v", err)
	}

	for snapName, snapst := range allStates {
		if err != nil {
			return InternalError("cannot retrieve info for snap %q: %v", snapName, err)
		}
		if len(snapst.Aliases) != 0 {
			snapAliases := make(map[string]aliasStatus)
			res[snapName] = snapAliases
			autoDisabled := snapst.AutoAliasesDisabled
			for alias, aliasTarget := range snapst.Aliases {
				aliasStatus := aliasStatus{
					Manual: aliasTarget.Manual,
					Auto:   aliasTarget.Auto,
				}
				status := "auto"
				tgt := aliasTarget.Effective(autoDisabled)
				if tgt == "" {
					status = "disabled"
					tgt = aliasTarget.Auto
				} else if aliasTarget.Manual != "" {
					status = "manual"
				}
				aliasStatus.Status = status
				aliasStatus.Command = snap.JoinSnapApp(snapName, tgt)
				snapAliases[alias] = aliasStatus
			}
		}
	}

	return SyncResponse(res, nil)
}

func getAppsInfo(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()

	opts := appInfoOptions{}
	switch sel := query.Get("select"); sel {
	case "":
		// nothing to do
	case "service":
		opts.service = true
	default:
		return BadRequest("invalid select parameter: %q", sel)
	}

	appInfos, rsp := appInfosFor(c.d.overlord.State(), strutil.CommaSeparatedList(query.Get("names")), opts)
	if rsp != nil {
		return rsp
	}

	sd := servicestate.NewStatusDecorator(progress.Null)

	clientAppInfos, err := clientutil.ClientAppInfosFromSnapAppInfos(appInfos, sd)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(clientAppInfos, nil)
}

func getLogs(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	n := 10
	if s := query.Get("n"); s != "" {
		m, err := strconv.ParseInt(s, 0, 32)
		if err != nil {
			return BadRequest(`invalid value for n: %q: %v`, s, err)
		}
		n = int(m)
	}
	follow := false
	if s := query.Get("follow"); s != "" {
		f, err := strconv.ParseBool(s)
		if err != nil {
			return BadRequest(`invalid value for follow: %q: %v`, s, err)
		}
		follow = f
	}

	// only services have logs for now
	opts := appInfoOptions{service: true}
	appInfos, rsp := appInfosFor(c.d.overlord.State(), strutil.CommaSeparatedList(query.Get("names")), opts)
	if rsp != nil {
		return rsp
	}
	if len(appInfos) == 0 {
		return AppNotFound("no matching services")
	}

	serviceNames := make([]string, len(appInfos))
	for i, appInfo := range appInfos {
		serviceNames[i] = appInfo.ServiceName()
	}

	sysd := systemd.New(dirs.GlobalRootDir, systemd.SystemMode, progress.Null)
	reader, err := sysd.LogReader(serviceNames, n, follow)
	if err != nil {
		return InternalError("cannot get logs: %v", err)
	}

	return &journalLineReaderSeqResponse{
		ReadCloser: reader,
		follow:     follow,
	}
}

func namesToSnapNames(inst *servicestate.Instruction) []string {
	seen := make(map[string]struct{}, len(inst.Names))
	for _, snapOrSnapDotApp := range inst.Names {
		snapName, _ := snap.SplitSnapApp(snapOrSnapDotApp)
		seen[snapName] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	// keep stable ordering
	sort.Strings(names)
	return names
}

func postApps(c *Command, r *http.Request, user *auth.UserState) Response {
	var inst servicestate.Instruction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("cannot decode request body into service operation: %v", err)
	}
	// XXX: decoder.More()
	if len(inst.Names) == 0 {
		// on POST, don't allow empty to mean all
		return BadRequest("cannot perform operation on services without a list of services to operate on")
	}

	st := c.d.overlord.State()
	appInfos, rsp := appInfosFor(st, inst.Names, appInfoOptions{service: true})
	if rsp != nil {
		return rsp
	}
	if len(appInfos) == 0 {
		// can't happen: appInfosFor with a non-empty list of services
		// shouldn't ever return an empty appInfos with no error response
		return InternalError("no services found")
	}

	tss, err := servicestateControl(st, appInfos, &inst, nil)
	if err != nil {
		// TODO: use errToResponse here too and introduce a proper error kind ?
		if _, ok := err.(servicestate.ServiceActionConflictError); ok {
			return Conflict(err.Error())
		}
		return BadRequest(err.Error())
	}
	st.Lock()
	defer st.Unlock()
	// names received in the request can be snap or snap.app, we need to
	// extract the actual snap names before associating them with a change
	chg := newChange(st, "service-control", fmt.Sprintf("Running service command"), tss, namesToSnapNames(&inst))
	st.EnsureBefore(0)
	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

var (
	stateOkayWarnings    = (*state.State).OkayWarnings
	stateAllWarnings     = (*state.State).AllWarnings
	statePendingWarnings = (*state.State).PendingWarnings
)

func ackWarnings(c *Command, r *http.Request, _ *auth.UserState) Response {
	defer r.Body.Close()
	var op struct {
		Action    string    `json:"action"`
		Timestamp time.Time `json:"timestamp"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&op); err != nil {
		return BadRequest("cannot decode request body into warnings operation: %v", err)
	}
	if op.Action != "okay" {
		return BadRequest("unknown warning action %q", op.Action)
	}
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	n := stateOkayWarnings(st, op.Timestamp)

	return SyncResponse(n, nil)
}

func getWarnings(c *Command, r *http.Request, _ *auth.UserState) Response {
	query := r.URL.Query()
	var all bool
	sel := query.Get("select")
	switch sel {
	case "all":
		all = true
	case "pending", "":
		all = false
	default:
		return BadRequest("invalid select parameter: %q", sel)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var ws []*state.Warning
	if all {
		ws = stateAllWarnings(st)
	} else {
		ws, _ = statePendingWarnings(st)
	}
	if len(ws) == 0 {
		// no need to confuse the issue
		return SyncResponse([]state.Warning{}, nil)
	}

	return SyncResponse(ws, nil)
}
