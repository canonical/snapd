// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
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
	snapConfCmd,
	snapHistoryCmd,
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
}

var (
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

	loginCmd = &Command{
		Path: "/v2/login",
		POST: loginUser,
	}

	logoutCmd = &Command{
		Path:   "/v2/logout",
		POST:   logoutUser,
		UserOK: true,
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
		Path:   "/v2/snaps",
		UserOK: true,
		GET:    getSnapsInfo,
		POST:   postSnaps,
	}

	snapCmd = &Command{
		Path:   "/v2/snaps/{name}",
		UserOK: true,
		GET:    getSnapInfo,
		POST:   postSnap,
	}

	snapConfCmd = &Command{
		Path: "/v2/snaps/{name}/conf",
		GET:  getSnapConf,
		PUT:  setSnapConf,
	}

	snapHistoryCmd = &Command{
		Path: "/v2/snaps/{name}/history",
		GET:  getSnapHistory,
	}

	interfacesCmd = &Command{
		Path:   "/v2/interfaces",
		UserOK: true,
		GET:    getInterfaces,
		POST:   changeInterfaces,
	}

	// TODO: allow to post assertions for UserOK? they are verified anyway
	assertsCmd = &Command{
		Path: "/v2/assertions",
		POST: doAssert,
	}

	assertsFindManyCmd = &Command{
		Path:   "/v2/assertions/{assertType}",
		UserOK: true,
		GET:    assertsFindMany,
	}

	stateChangeCmd = &Command{
		Path:   "/v2/changes/{id}",
		UserOK: true,
		GET:    getChange,
		POST:   abortChange,
	}

	stateChangesCmd = &Command{
		Path:   "/v2/changes",
		UserOK: true,
		GET:    getChanges,
	}

	createUserCmd = &Command{
		Path:   "/v2/create-user",
		UserOK: false,
		POST:   postCreateUser,
	}

	buyCmd = &Command{
		Path:   "/v2/buy",
		UserOK: false,
		POST:   postBuy,
	}

	readyToBuyCmd = &Command{
		Path:   "/v2/buy/ready",
		UserOK: false,
		GET:    readyToBuy,
	}

	snapctlCmd = &Command{
		Path:   "/v2/snapctl",
		SnapOK: true,
		POST:   runSnapctl,
	}

	usersCmd = &Command{
		Path:   "/v2/users",
		UserOK: false,
		GET:    getUsers,
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
)

func tbd(c *Command, r *http.Request, user *auth.UserState) Response {
	return SyncResponse([]string{"TBD"}, nil)
}

func sysInfo(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	st.Unlock()
	if err != nil && err != state.ErrNoState {
		return InternalError("cannot get user auth data: %s", err)
	}

	m := map[string]interface{}{
		"series":     release.Series,
		"version":    c.d.Version,
		"os-release": release.ReleaseInfo,
		"on-classic": release.OnClassic,
		"managed":    len(users) > 0,

		"kernel-version": release.KernelVersion(),
	}

	// TODO: set the store-id here from the model information
	if storeID := os.Getenv("UBUNTU_STORE_ID"); storeID != "" {
		m["store"] = storeID
	}

	return SyncResponse(m, nil)
}

// userResponseData contains the data releated to user creation/login/query
type userResponseData struct {
	ID       int      `json:"id,omitempty"`
	Username string   `json:"username,omitempty"`
	Email    string   `json:"email,omitempty"`
	SSHKeys  []string `json:"ssh-keys,omitempty"`

	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

var isEmailish = regexp.MustCompile(`.@.*\..`).MatchString

func loginUser(c *Command, r *http.Request, user *auth.UserState) Response {
	var loginData struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Otp      string `json:"otp"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&loginData); err != nil {
		return BadRequest("cannot decode login data from request body: %v", err)
	}

	if loginData.Email == "" && isEmailish(loginData.Username) {
		// for backwards compatibility, if no email is provided assume username is the email
		loginData.Email = loginData.Username
		loginData.Username = ""
	}

	if loginData.Email == "" && user != nil && user.Email != "" {
		loginData.Email = user.Email
	}

	// the "username" needs to look a lot like an email address
	if !isEmailish(loginData.Email) {
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: "please use a valid email address.",
				Kind:    errorKindInvalidAuthData,
				Value:   map[string][]string{"email": {"invalid"}},
			},
			Status: http.StatusBadRequest,
		}, nil)
	}

	macaroon, discharge, err := store.LoginUser(loginData.Email, loginData.Password, loginData.Otp)
	switch err {
	case store.ErrAuthenticationNeeds2fa:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Kind:    errorKindTwoFactorRequired,
				Message: err.Error(),
			},
			Status: http.StatusUnauthorized,
		}, nil)
	case store.Err2faFailed:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Kind:    errorKindTwoFactorFailed,
				Message: err.Error(),
			},
			Status: http.StatusUnauthorized,
		}, nil)
	default:
		if err, ok := err.(store.ErrInvalidAuthData); ok {
			return SyncResponse(&resp{
				Type: ResponseTypeError,
				Result: &errorResult{
					Message: err.Error(),
					Kind:    errorKindInvalidAuthData,
					Value:   err,
				},
				Status: http.StatusBadRequest,
			}, nil)
		}
		return Unauthorized(err.Error())
	case nil:
		// continue
	}
	overlord := c.d.overlord
	state := overlord.State()
	state.Lock()
	if user != nil {
		// local user logged-in, set its store macaroons
		user.StoreMacaroon = macaroon
		user.StoreDischarges = []string{discharge}
		err = auth.UpdateUser(state, user)
	} else {
		user, err = auth.NewUser(state, loginData.Username, loginData.Email, macaroon, []string{discharge})
	}
	state.Unlock()
	if err != nil {
		return InternalError("cannot persist authentication details: %v", err)
	}

	result := userResponseData{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}
	return SyncResponse(result, nil)
}

func logoutUser(c *Command, r *http.Request, user *auth.UserState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if user == nil {
		return BadRequest("not logged in")
	}
	err := auth.RemoveUser(state, user.ID)
	if err != nil {
		return InternalError(err.Error())
	}

	return SyncResponse(nil, nil)
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
	for _, field := range strings.Split(authorizationData[1], ",") {
		field := strings.TrimSpace(field)
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
			return NotFound("cannot find %q snap", name)
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

	result := webify(mapLocal(about), url.String())

	return SyncResponse(result, nil)
}

func webify(result map[string]interface{}, resource string) map[string]interface{} {
	result["resource"] = resource

	icon, ok := result["icon"].(string)
	if !ok || icon == "" || strings.HasPrefix(icon, "http") {
		return result
	}
	result["icon"] = ""

	route := appIconCmd.d.router.Get(appIconCmd.Path)
	if route != nil {
		name, _ := result["name"].(string)
		url, err := route.URL("name", name)
		if err == nil {
			result["icon"] = url.String()
		}
	}

	return result
}

func getStore(c *Command) snapstate.StoreService {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	return snapstate.Store(st)
}

func getSections(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	theStore := getStore(c)

	sections, err := theStore.Sections(user)
	switch err {
	case nil:
		// pass
	case store.ErrEmptyQuery, store.ErrBadQuery:
		return BadRequest("%v", err)
	case store.ErrUnauthenticated:
		return Unauthorized("%v", err)
	default:
		return InternalError("%v", err)
	}

	return SyncResponse(sections, &Meta{})
}

func searchStore(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}
	query := r.URL.Query()
	q := query.Get("q")
	section := query.Get("section")
	name := query.Get("name")
	private := false
	prefix := false

	if name != "" {
		if q != "" {
			return BadRequest("cannot use 'q' and 'name' together")
		}

		if name[len(name)-1] != '*' {
			return findOne(c, r, user, name)
		}

		prefix = true
		q = name[:len(name)-1]
	}

	if sel := query.Get("select"); sel != "" {
		switch sel {
		case "refresh":
			if prefix {
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

	theStore := getStore(c)
	found, err := theStore.Find(&store.Search{
		Query:   q,
		Section: section,
		Private: private,
		Prefix:  prefix,
	}, user)
	switch err {
	case nil:
		// pass
	case store.ErrEmptyQuery, store.ErrBadQuery:
		return BadRequest("%v", err)
	case store.ErrUnauthenticated:
		return Unauthorized(err.Error())
	default:
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
		Name:     name,
		Channel:  "",
		Revision: snap.R(0),
	}
	snapInfo, err := theStore.SnapInfo(spec, user)
	if err != nil {
		if err == store.ErrSnapNotFound {
			return NotFound(err.Error())
		}
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
		url, err := route.URL("name", x.Name())
		if err != nil {
			logger.Noticef("Cannot build URL for snap %q revision %s: %v", x.Name(), x.Revision, err)
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
		nsl := strings.Split(ns, ",")
		wanted = make(map[string]bool, len(nsl))
		for _, name := range nsl {
			name = strings.TrimSpace(name)
			if len(name) > 0 {
				wanted[name] = true
			}
		}
	}

	found, err := allLocalSnapInfos(c.d.overlord.State(), all, wanted)
	if err != nil {
		return InternalError("cannot list local snaps! %v", err)
	}

	results := make([]*json.RawMessage, len(found))

	for i, x := range found {
		name := x.info.Name()
		rev := x.info.Revision

		url, err := route.URL("name", name)
		if err != nil {
			logger.Noticef("Cannot build URL for snap %q revision %s: %v", name, rev, err)
			continue
		}

		data, err := json.Marshal(webify(mapLocal(x), url.String()))
		if err != nil {
			return InternalError("cannot serialize snap %q revision %s: %v", name, rev, err)
		}
		raw := json.RawMessage(data)
		results[i] = &raw
	}

	return SyncResponse(results, &Meta{Sources: []string{"local"}})
}

func resultHasType(r map[string]interface{}, allowedTypes []string) bool {
	for _, t := range allowedTypes {
		if r["type"] == t {
			return true
		}
	}
	return false
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

type snapInstruction struct {
	progress.NullProgress
	Action           string        `json:"action"`
	Channel          string        `json:"channel"`
	Revision         snap.Revision `json:"revision"`
	DevMode          bool          `json:"devmode"`
	JailMode         bool          `json:"jailmode"`
	Classic          bool          `json:"classic"`
	IgnoreValidation bool          `json:"ignore-validation"`
	// dropping support temporarely until flag confusion is sorted,
	// this isn't supported by client atm anyway
	LeaveOld bool         `json:"temp-dropped-leave-old"`
	License  *licenseData `json:"license"`
	Snaps    []string     `json:"snaps"`

	// The fields below should not be unmarshalled into. Do not export them.
	userID int
}

func (inst *snapInstruction) modeFlags() (snapstate.Flags, error) {
	return modeFlags(inst.DevMode, inst.JailMode, inst.Classic)
}

var (
	snapstateCoreInfo          = snapstate.CoreInfo
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

	assertstateRefreshSnapDeclarations = assertstate.RefreshSnapDeclarations
)

func ensureStateSoonImpl(st *state.State) {
	st.EnsureBefore(0)
}

var ensureStateSoon = ensureStateSoonImpl

var errNothingToInstall = errors.New("nothing to install")

const oldDefaultSnapCoreName = "ubuntu-core"
const defaultCoreSnapName = "core"

func ensureUbuntuCore(st *state.State, targetSnap string, userID int) (*state.TaskSet, error) {
	if targetSnap == defaultCoreSnapName || targetSnap == oldDefaultSnapCoreName {
		return nil, errNothingToInstall
	}

	_, err := snapstateCoreInfo(st)
	if err != state.ErrNoState {
		return nil, err
	}

	return snapstateInstall(st, defaultCoreSnapName, "stable", snap.R(0), userID, snapstate.Flags{})
}

func withEnsureUbuntuCore(st *state.State, targetSnap string, userID int, install func() (*state.TaskSet, error)) ([]*state.TaskSet, error) {
	ubuCoreTs, err := ensureUbuntuCore(st, targetSnap, userID)
	if err != nil && err != errNothingToInstall {
		return nil, err
	}

	ts, err := install()
	if err != nil {
		return nil, err
	}

	// ensure main install waits on ubuntu core install
	if ubuCoreTs != nil {
		ts.WaitAll(ubuCoreTs)
		return []*state.TaskSet{ubuCoreTs, ts}, nil
	}

	return []*state.TaskSet{ts}, nil
}

var errDevJailModeConflict = errors.New("cannot use devmode and jailmode flags together")
var errClassicDevmodeConflict = errors.New("cannot use classic and devmode flags together")
var errNoJailMode = errors.New("this system cannot honour the jailmode flag")

func modeFlags(devMode, jailMode, classic bool) (snapstate.Flags, error) {
	flags := snapstate.Flags{}
	devModeOS := release.ReleaseInfo.ForceDevMode()
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

func snapUpdateMany(inst *snapInstruction, st *state.State) (msg string, updated []string, tasksets []*state.TaskSet, err error) {
	// we need refreshed snap-declarations to enforce refresh-control as best as we can, this also ensures that snap-declarations and their prerequisite assertions are updated regularly
	if err := assertstateRefreshSnapDeclarations(st, inst.userID); err != nil {
		return "", nil, nil, err
	}

	updated, tasksets, err = snapstateUpdateMany(st, inst.Snaps, inst.userID)
	if err != nil {
		return "", nil, nil, err
	}

	switch len(updated) {
	case 0:
		if len(inst.Snaps) != 0 {
			// TRANSLATORS: the %s is a comma-separated list of quoted snap names
			msg = fmt.Sprintf(i18n.G("Refresh snaps %s: no updates"), strutil.Quoted(inst.Snaps))
		} else {
			msg = fmt.Sprintf(i18n.G("Refresh all snaps: no updates"))
		}
	case 1:
		msg = fmt.Sprintf(i18n.G("Refresh snap %q"), updated[0])
	default:
		quoted := strutil.Quoted(updated)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Refresh snaps %s"), quoted)
	}

	return msg, updated, tasksets, nil
}

func snapInstallMany(inst *snapInstruction, st *state.State) (msg string, installed []string, tasksets []*state.TaskSet, err error) {
	installed, tasksets, err = snapstateInstallMany(st, inst.Snaps, inst.userID)
	if err != nil {
		return "", nil, nil, err
	}

	switch len(inst.Snaps) {
	case 0:
		return "", nil, nil, fmt.Errorf("cannot install zero snaps")
	case 1:
		msg = fmt.Sprintf(i18n.G("Install snap %q"), inst.Snaps[0])
	default:
		quoted := strutil.Quoted(inst.Snaps)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Install snaps %s"), quoted)
	}

	return msg, installed, tasksets, nil
}

func snapInstall(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	flags, err := inst.modeFlags()
	if err != nil {
		return "", nil, err
	}

	logger.Noticef("Installing snap %q revision %s", inst.Snaps[0], inst.Revision)

	tsets, err := withEnsureUbuntuCore(st, inst.Snaps[0], inst.userID,
		func() (*state.TaskSet, error) {
			return snapstateInstall(st, inst.Snaps[0], inst.Channel, inst.Revision, inst.userID, flags)
		},
	)
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap"), inst.Snaps[0])
	if inst.Channel != "stable" && inst.Channel != "" {
		msg = fmt.Sprintf(i18n.G("Install %q snap from %q channel"), inst.Snaps[0], inst.Channel)
	}
	return msg, tsets, nil
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

	// we need refreshed snap-declarations to enforce refresh-control as best as we can
	if err = assertstateRefreshSnapDeclarations(st, inst.userID); err != nil {
		return "", nil, err
	}

	ts, err := snapstateUpdate(st, inst.Snaps[0], inst.Channel, inst.Revision, inst.userID, flags)
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Refresh %q snap"), inst.Snaps[0])
	if inst.Channel != "stable" && inst.Channel != "" {
		msg = fmt.Sprintf(i18n.G("Refresh %q snap from %q channel"), inst.Snaps[0], inst.Channel)
	}

	return msg, []*state.TaskSet{ts}, nil
}

func snapRemoveMany(inst *snapInstruction, st *state.State) (msg string, removed []string, tasksets []*state.TaskSet, err error) {
	removed, tasksets, err = snapstateRemoveMany(st, inst.Snaps)
	if err != nil {
		return "", nil, nil, err
	}

	switch len(inst.Snaps) {
	case 0:
		return "", nil, nil, fmt.Errorf("cannot remove zero snaps")
	case 1:
		msg = fmt.Sprintf(i18n.G("Remove snap %q"), inst.Snaps[0])
	default:
		quoted := strutil.Quoted(inst.Snaps)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Remove snaps %s"), quoted)
	}

	return msg, removed, tasksets, nil
}

func snapRemove(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	ts, err := snapstate.Remove(st, inst.Snaps[0], inst.Revision)
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

type snapActionFunc func(*snapInstruction, *state.State) (string, []*state.TaskSet, error)

var snapInstructionDispTable = map[string]snapActionFunc{
	"install": snapInstall,
	"refresh": snapUpdate,
	"remove":  snapRemove,
	"revert":  snapRevert,
	"enable":  snapEnable,
	"disable": snapDisable,
}

func (inst *snapInstruction) dispatch() snapActionFunc {
	if len(inst.Snaps) != 1 {
		logger.Panicf("dispatch only handles single-snap ops; got %d", len(inst.Snaps))
	}
	return snapInstructionDispTable[inst.Action]
}

func (inst *snapInstruction) errToResponse(err error) Response {
	result := &errorResult{Message: err.Error()}

	switch err := err.(type) {
	case *snap.AlreadyInstalledError:
		result.Kind = errorKindSnapAlreadyInstalled
	case *snap.NotInstalledError:
		result.Kind = errorKindSnapNotInstalled
	case *snap.NoUpdateAvailableError:
		result.Kind = errorKindSnapNoUpdateAvailable
	case *snapstate.ErrSnapNeedsMode:
		result.Kind = errorKindSnapNeedsMode
		result.Value = err.Mode
	case *snapstate.ErrSnapNeedsClassicSystem:
		result.Kind = errorKindSnapNeedsClassicSystem
	default:
		return BadRequest("cannot %s %q: %v", inst.Action, inst.Snaps[0], err)
	}

	return SyncResponse(&resp{
		Type:   ResponseTypeError,
		Result: result,
		Status: http.StatusBadRequest,
	}, nil)
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

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	vars := muxVars(r)
	inst.Snaps = []string{vars["name"]}

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
				Kind:    errorKindNotSnap,
			},
			Status: http.StatusBadRequest,
		}, nil)
	}
	if err != nil {
		return BadRequest("cannot read snap info for %s: %s", trydir, err)
	}

	var userID int
	if user != nil {
		userID = user.ID
	}
	tsets, err := withEnsureUbuntuCore(st, info.Name(), userID,
		func() (*state.TaskSet, error) {
			return snapstateTryPath(st, info.Name(), trydir, flags)
		},
	)
	if err != nil {
		return BadRequest("cannot try %s: %s", trydir, err)
	}

	msg := fmt.Sprintf(i18n.G("Try %q snap from %s"), info.Name(), trydir)
	chg := newChange(st, "try-snap", msg, tsets, []string{info.Name()})
	chg.Set("api-data", map[string]string{"snap-name": info.Name()})

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

	if inst.Channel != "" || !inst.Revision.Unset() || inst.DevMode || inst.JailMode {
		return BadRequest("unsupported option provided for multi-snap operation")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	var msg string
	var affected []string
	var tsets []*state.TaskSet
	var err error
	switch inst.Action {
	case "refresh":
		msg, affected, tsets, err = snapUpdateMany(&inst, st)
	case "install":
		msg, affected, tsets, err = snapInstallMany(&inst, st)
	case "remove":
		msg, affected, tsets, err = snapRemoveMany(&inst, st)
	default:
		return BadRequest("unsupported multi-snap operation %q", inst.Action)
	}
	if err != nil {
		return InternalError("cannot %s %q: %v", inst.Action, inst.Snaps, err)
	}

	var chg *state.Change
	if len(tsets) == 0 {
		chg = st.NewChange(inst.Action+"-snap", msg)
		chg.SetStatus(state.DoneStatus)
	} else {
		chg = newChange(st, inst.Action+"-snap", msg, tsets, affected)
		ensureStateSoon(st)
	}
	chg.Set("api-data", map[string]interface{}{"snap-names": affected})

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
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
	flags.RemoveSnapPath = true

	if len(form.Value["action"]) > 0 && form.Value["action"][0] == "try" {
		if len(form.Value["snap-path"]) == 0 {
			return BadRequest("need 'snap-path' value in form")
		}
		return trySnap(c, r, user, form.Value["snap-path"][0], flags)
	}

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
	tmpf, err := ioutil.TempFile("", "snapd-sideload-pkg-")
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

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var snapName string
	var sideInfo *snap.SideInfo

	if !dangerousOK {
		si, err := snapasserts.DeriveSideInfo(tempPath, assertstate.DB(st))
		switch err {
		case nil:
			snapName = si.RealName
			sideInfo = si
		case asserts.ErrNotFound:
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
		snapName = info.Name()
		sideInfo = &snap.SideInfo{RealName: snapName}
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap from file"), snapName)
	if origPath != "" {
		msg = fmt.Sprintf(i18n.G("Install %q snap from file %q"), snapName, origPath)
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	tsets, err := withEnsureUbuntuCore(st, snapName, userID,
		func() (*state.TaskSet, error) {
			return snapstateInstallPath(st, sideInfo, tempPath, "", flags)
		},
	)
	if err != nil {
		return InternalError("cannot install snap file: %v", err)
	}

	chg := newChange(st, "install-snap", msg, tsets, []string{snapName})
	chg.Set("api-data", map[string]string{"snap-name": snapName})

	ensureStateSoon(st)

	// only when the unlock succeeds (as opposed to panicing) is the handoff done
	// but this is good enough
	changeTriggered = true

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func unsafeReadSnapInfoImpl(snapPath string) (*snap.Info, error) {
	// Condider using DeriveSideInfo before falling back to this!
	snapf, err := snap.Open(snapPath)
	if err != nil {
		return nil, err
	}
	return snap.ReadInfoFromSnapFile(snapf, nil)
}

var unsafeReadSnapInfo = unsafeReadSnapInfoImpl

func iconGet(st *state.State, name string) Response {
	about, err := localSnapInfo(st, name)
	if err != nil {
		if err == errNoSnap {
			return NotFound("cannot find snap %q", name)
		}
		return InternalError("%v", err)
	}

	path := filepath.Clean(snapIcon(about.info))
	if !strings.HasPrefix(path, dirs.SnapMountDir) {
		// XXX: how could this happen?
		return BadRequest("requested icon is not in snap path")
	}

	return FileResponse(path)
}

func appIconGet(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	return iconGet(c.d.overlord.State(), name)
}

func getSnapConf(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	snapName := vars["name"]

	keys := strings.Split(r.URL.Query().Get("keys"), ",")
	if len(keys) == 0 {
		return BadRequest("cannot obtain configuration: no keys supplied")
	}

	s := c.d.overlord.State()
	s.Lock()
	tr := config.NewTransaction(s)
	s.Unlock()

	currentConfValues := make(map[string]interface{})
	for _, key := range keys {
		var value interface{}
		if err := tr.Get(snapName, key, &value); err != nil {
			return BadRequest("%s", err)
		}

		currentConfValues[key] = value
	}

	return SyncResponse(currentConfValues, nil)
}

func setSnapConf(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	snapName := vars["name"]

	var patchValues map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&patchValues); err != nil {
		return BadRequest("cannot decode request body into patch values: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err == state.ErrNoState {
		return NotFound("cannot find %q snap", snapName)
	} else if err != nil {
		return InternalError("%v", err)
	}

	taskset := configstate.Configure(st, snapName, patchValues)

	summary := fmt.Sprintf("Change configuration of %q snap", snapName)
	change := newChange(st, "configure-snap", summary, []*state.TaskSet{taskset}, []string{snapName})

	st.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: change.ID()})
}

func getSnapHistory(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	url, err := route.URL("name", name)
	if err != nil {
		return InternalError("cannot build URL for %q snap: %v", name, err)
	}

	var history []map[string]interface{}
	snapRevisions, err := allLocalRevisions(c.d.overlord.State(), name)
	if err != nil {
		if err == state.ErrNoState {
			return NotFound(err.Error())
		}
		return InternalError(err.Error())
	}

	for i := len(snapRevisions) - 1; i >= 0; i-- {
		history = append(history, webify(mapLocal(snapRevisions[i]), url.String()))
	}

	return SyncResponse(history, nil)
}

// getInterfaces returns all plugs and slots.
func getInterfaces(c *Command, r *http.Request, user *auth.UserState) Response {
	repo := c.d.overlord.InterfaceManager().Repository()
	return SyncResponse(repo.Interfaces(), nil)
}

// plugJSON aids in marshaling Plug into JSON.
type plugJSON struct {
	Snap        string                 `json:"snap"`
	Name        string                 `json:"plug"`
	Interface   string                 `json:"interface"`
	Attrs       map[string]interface{} `json:"attrs,omitempty"`
	Apps        []string               `json:"apps,omitempty"`
	Label       string                 `json:"label"`
	Connections []interfaces.SlotRef   `json:"connections,omitempty"`
}

// slotJSON aids in marshaling Slot into JSON.
type slotJSON struct {
	Snap        string                 `json:"snap"`
	Name        string                 `json:"slot"`
	Interface   string                 `json:"interface"`
	Attrs       map[string]interface{} `json:"attrs,omitempty"`
	Apps        []string               `json:"apps,omitempty"`
	Label       string                 `json:"label"`
	Connections []interfaces.PlugRef   `json:"connections,omitempty"`
}

// interfaceAction is an action performed on the interface system.
type interfaceAction struct {
	Action string     `json:"action"`
	Plugs  []plugJSON `json:"plugs,omitempty"`
	Slots  []slotJSON `json:"slots,omitempty"`
}

// changeInterfaces controls the interfaces system.
// Plugs can be connected to and disconnected from slots.
// When enableInternalInterfaceActions is true plugs and slots can also be
// explicitly added and removed.
func changeInterfaces(c *Command, r *http.Request, user *auth.UserState) Response {
	var a interfaceAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&a); err != nil {
		return BadRequest("cannot decode request body into an interface action: %v", err)
	}
	if a.Action == "" {
		return BadRequest("interface action not specified")
	}
	if !c.d.enableInternalInterfaceActions && a.Action != "connect" && a.Action != "disconnect" {
		return BadRequest("internal interface actions are disabled")
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
	var taskset *state.TaskSet
	var err error

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	switch a.Action {
	case "connect":
		var connRef interfaces.ConnRef
		repo := c.d.overlord.InterfaceManager().Repository()
		connRef, err = repo.ResolveConnect(a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		if err == nil {
			summary = fmt.Sprintf("Connect %s:%s to %s:%s", connRef.PlugRef.Snap, connRef.PlugRef.Name, connRef.SlotRef.Snap, connRef.SlotRef.Name)
			taskset, err = ifacestate.Connect(state, connRef.PlugRef.Snap, connRef.PlugRef.Name, connRef.SlotRef.Snap, connRef.SlotRef.Name)
		}
	case "disconnect":
		summary = fmt.Sprintf("Disconnect %s:%s from %s:%s", a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		taskset, err = ifacestate.Disconnect(state, a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
	}
	if err != nil {
		return BadRequest("%v", err)
	}

	change := state.NewChange(a.Action+"-snap", summary)
	change.Set("snap-names", []string{a.Plugs[0].Snap, a.Slots[0].Snap})
	change.AddAll(taskset)

	state.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: change.ID()})
}

func doAssert(c *Command, r *http.Request, user *auth.UserState) Response {
	batch := assertstate.NewBatch()
	_, err := batch.AddStream(r.Body)
	if err != nil {
		return BadRequest("cannot decode request body into assertions: %v", err)
	}

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if err := batch.Commit(state); err != nil {
		return BadRequest("assert failed: %v", err)
	}
	// TODO: what more info do we want to return on success?
	return &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
	}
}

func assertsFindMany(c *Command, r *http.Request, user *auth.UserState) Response {
	assertTypeName := muxVars(r)["assertType"]
	assertType := asserts.Type(assertTypeName)
	if assertType == nil {
		return BadRequest("invalid assert type: %q", assertTypeName)
	}
	headers := map[string]string{}
	q := r.URL.Query()
	for k := range q {
		headers[k] = q.Get(k)
	}

	state := c.d.overlord.State()
	state.Lock()
	db := assertstate.DB(state)
	state.Unlock()

	assertions, err := db.FindMany(assertType, headers)
	if err == asserts.ErrNotFound {
		return AssertResponse(nil, true)
	} else if err != nil {
		return InternalError("searching assertions failed: %v", err)
	}
	return AssertResponse(assertions, true)
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

			for _, snapName := range snapNames {
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
	postCreateUserUcrednetGetUID = ucrednetGetUID
	storeUserInfo                = store.UserInfo
	osutilAddUser                = osutil.AddUser
)

func getUserDetailsFromStore(email string) (string, *osutil.AddUserOptions, error) {
	v, err := storeUserInfo(email)
	if err != nil {
		return "", nil, fmt.Errorf("cannot create user %q: %s", email, err)
	}
	if len(v.SSHKeys) == 0 {
		return "", nil, fmt.Errorf("cannot create user for %q: no ssh keys found", email)
	}

	gecos := fmt.Sprintf("%s,%s", email, v.OpenIDIdentifier)
	opts := &osutil.AddUserOptions{
		SSHKeys: v.SSHKeys,
		Gecos:   gecos,
	}
	return v.Username, opts, nil
}

func createAllKnownSystemUsers(st *state.State, createData *postUserCreateData) Response {
	var createdUsers []userResponseData

	st.Lock()
	db := assertstate.DB(st)
	modelAs, err := devicestate.Model(st)
	st.Unlock()
	if err != nil {
		return InternalError("cannot get model assertion")
	}

	headers := map[string]string{
		"brand-id": modelAs.BrandID(),
	}
	st.Lock()
	assertions, err := db.FindMany(asserts.SystemUserType, headers)
	st.Unlock()
	if err != nil && err != asserts.ErrNotFound {
		return BadRequest("cannot find system-user assertion: %s", err)
	}

	for _, as := range assertions {
		email := as.(*asserts.SystemUser).Email()
		// we need to use getUserDetailsFromAssertion as this verifies
		// the assertion against the current brand/model/time
		username, opts, err := getUserDetailsFromAssertion(st, email)
		if err != nil {
			logger.Noticef("ignoring system-user assertion for %q: %s", email, err)
			continue
		}
		// ignore already existing users
		if _, err := user.Lookup(username); err == nil {
			continue
		}

		// FIXME: duplicated code
		opts.Sudoer = createData.Sudoer
		opts.ExtraUsers = !release.OnClassic

		if err := osutilAddUser(username, opts); err != nil {
			return InternalError("cannot add user %q: %s", username, err)
		}
		if err := setupLocalUser(st, username, email); err != nil {
			return InternalError("%s", err)
		}
		createdUsers = append(createdUsers, userResponseData{
			Username: username,
			SSHKeys:  opts.SSHKeys,
		})
	}

	return SyncResponse(createdUsers, nil)
}

func getUserDetailsFromAssertion(st *state.State, email string) (string, *osutil.AddUserOptions, error) {
	errorPrefix := fmt.Sprintf("cannot add system-user %q: ", email)

	st.Lock()
	db := assertstate.DB(st)
	modelAs, err := devicestate.Model(st)
	st.Unlock()
	if err != nil {
		return "", nil, fmt.Errorf(errorPrefix+"cannot get model assertion: %s", err)
	}

	brandID := modelAs.BrandID()
	series := modelAs.Series()
	model := modelAs.Model()

	a, err := db.Find(asserts.SystemUserType, map[string]string{
		"brand-id": brandID,
		"email":    email,
	})
	if err != nil {
		return "", nil, fmt.Errorf(errorPrefix+"%v", err)
	}
	// the asserts package guarantees that this cast will work
	su := a.(*asserts.SystemUser)

	// cross check that the assertion is valid for the given series/model
	contains := func(needle string, haystack []string) bool {
		for _, s := range haystack {
			if needle == s {
				return true
			}
		}
		return false
	}
	// check that the signer of the assertion is one of the accepted ones
	sysUserAuths := modelAs.SystemUserAuthority()
	if len(sysUserAuths) > 0 && !contains(su.AuthorityID(), sysUserAuths) {
		return "", nil, fmt.Errorf(errorPrefix+"%q not in accepted authorities %q", email, su.AuthorityID(), sysUserAuths)
	}
	if len(su.Series()) > 0 && !contains(series, su.Series()) {
		return "", nil, fmt.Errorf(errorPrefix+"%q not in series %q", email, series, su.Series())
	}
	if len(su.Models()) > 0 && !contains(model, su.Models()) {
		return "", nil, fmt.Errorf(errorPrefix+"%q not in models %q", model, su.Models())
	}
	if !su.ValidAt(time.Now()) {
		return "", nil, fmt.Errorf(errorPrefix + "assertion not valid anymore")
	}

	gecos := fmt.Sprintf("%s,%s", email, su.Name())
	opts := &osutil.AddUserOptions{
		SSHKeys:  su.SSHKeys(),
		Gecos:    gecos,
		Password: su.Password(),
	}
	return su.Username(), opts, nil
}

type postUserCreateData struct {
	Email        string `json:"email"`
	Sudoer       bool   `json:"sudoer"`
	Known        bool   `json:"known"`
	ForceManaged bool   `json:"force-managed"`
}

var userLookup = user.Lookup

func setupLocalUser(st *state.State, username, email string) error {
	user, err := userLookup(username)
	if err != nil {
		return fmt.Errorf("cannot lookup user %q: %s", username, err)
	}
	uid, err := strconv.Atoi(user.Uid)
	if err != nil {
		return fmt.Errorf("cannot get uid of user %q: %s", username, err)
	}
	gid, err := strconv.Atoi(user.Gid)
	if err != nil {
		return fmt.Errorf("cannot get gid of user %q: %s", username, err)
	}
	authDataFn := filepath.Join(user.HomeDir, ".snap", "auth.json")
	if err := osutil.MkdirAllChown(filepath.Dir(authDataFn), 0700, uid, gid); err != nil {
		return err
	}

	// setup new user, local-only
	st.Lock()
	authUser, err := auth.NewUser(st, username, email, "", nil)
	st.Unlock()
	if err != nil {
		return fmt.Errorf("cannot persist authentication details: %v", err)
	}
	// store macaroon auth in auth.json in the new users home dir
	outStr, err := json.Marshal(struct {
		Macaroon string `json:"macaroon"`
	}{
		Macaroon: authUser.Macaroon,
	})
	if err != nil {
		return fmt.Errorf("cannot marshal auth data: %s", err)
	}
	if err := osutil.AtomicWriteFileChown(authDataFn, []byte(outStr), 0600, 0, uid, gid); err != nil {
		return fmt.Errorf("cannot write auth file %q: %s", authDataFn, err)
	}

	return nil
}

func postCreateUser(c *Command, r *http.Request, user *auth.UserState) Response {
	uid, err := postCreateUserUcrednetGetUID(r.RemoteAddr)
	if err != nil {
		return BadRequest("cannot get ucrednet uid: %v", err)
	}
	if uid != 0 {
		return BadRequest("cannot use create-user as non-root")
	}

	var createData postUserCreateData

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&createData); err != nil {
		return BadRequest("cannot decode create-user data from request body: %v", err)
	}

	// verify request
	st := c.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	st.Unlock()
	if err != nil {
		return InternalError("cannot get user count: %s", err)
	}

	if !createData.ForceManaged {
		if len(users) > 0 {
			return BadRequest("cannot create user: device already managed")
		}
		if release.OnClassic {
			return BadRequest("cannot create user: device is a classic system")
		}
	}

	// special case: the user requested the creation of all known
	// system-users
	if createData.Email == "" && createData.Known {
		return createAllKnownSystemUsers(c.d.overlord.State(), &createData)
	}
	if createData.Email == "" {
		return BadRequest("cannot create user: 'email' field is empty")
	}

	var username string
	var opts *osutil.AddUserOptions
	if createData.Known {
		username, opts, err = getUserDetailsFromAssertion(st, createData.Email)
	} else {
		username, opts, err = getUserDetailsFromStore(createData.Email)
	}
	if err != nil {
		return BadRequest("%s", err)
	}

	// FIXME: duplicated code
	opts.Sudoer = createData.Sudoer
	opts.ExtraUsers = !release.OnClassic

	if err := osutilAddUser(username, opts); err != nil {
		return BadRequest("cannot create user %s: %s", username, err)
	}

	if err := setupLocalUser(c.d.overlord.State(), username, createData.Email); err != nil {
		return InternalError("%s", err)
	}

	return SyncResponse(&userResponseData{
		Username: username,
		SSHKeys:  opts.SSHKeys,
	}, nil)
}

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
				Kind:    errorKindLoginRequired,
			},
			Status: http.StatusBadRequest,
		}, nil)
	case store.ErrTOSNotAccepted:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    errorKindTermsNotAccepted,
			},
			Status: http.StatusBadRequest,
		}, nil)
	case store.ErrNoPaymentMethods:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    errorKindNoPaymentMethods,
			},
			Status: http.StatusBadRequest,
		}, nil)
	case store.ErrPaymentDeclined:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    errorKindPaymentDeclined,
			},
			Status: http.StatusBadRequest,
		}, nil)
	default:
		return InternalError("%v", err)
	}
}

func postBuy(c *Command, r *http.Request, user *auth.UserState) Response {
	var opts store.BuyOptions

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
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&snapctlOptions); err != nil {
		return BadRequest("cannot decode snapctl request: %s", err)
	}

	if len(snapctlOptions.Args) == 0 {
		return BadRequest("snapctl cannot run without args")
	}

	// Right now snapctl is only used for hooks. If at some point it grows
	// beyond that, this probably shouldn't go straight to the HookManager.
	context, _ := c.d.overlord.HookManager().Context(snapctlOptions.ContextID)
	stdout, stderr, err := ctlcmd.Run(context, snapctlOptions.Args)
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			stdout = []byte(e.Error())
		} else {
			return BadRequest("error running snapctl: %s", err)
		}
	}

	result := map[string]string{
		"stdout": string(stdout),
		"stderr": string(stderr),
	}

	return SyncResponse(result, nil)
}

func getUsers(c *Command, r *http.Request, user *auth.UserState) Response {
	uid, err := postCreateUserUcrednetGetUID(r.RemoteAddr)
	if err != nil {
		return BadRequest("cannot get ucrednet uid: %v", err)
	}
	if uid != 0 {
		return BadRequest("cannot get users as non-root")
	}

	st := c.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	st.Unlock()
	if err != nil {
		return InternalError("cannot get users: %s", err)
	}

	resp := make([]userResponseData, len(users))
	for i, u := range users {
		resp[i] = userResponseData{
			Username: u.Username,
			Email:    u.Email,
			ID:       u.ID,
		}
	}
	return SyncResponse(resp, nil)
}

// aliasAction is an action performed on aliases
type aliasAction struct {
	Action  string   `json:"action"`
	Snap    string   `json:"snap"`
	Aliases []string `json:"aliases"`
}

func changeAliases(c *Command, r *http.Request, user *auth.UserState) Response {
	var a aliasAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&a); err != nil {
		return BadRequest("cannot decode request body into an alias action: %v", err)
	}
	if len(a.Aliases) == 0 {
		return BadRequest("at least one alias name is required")
	}

	var summary string
	var taskset *state.TaskSet
	var err error

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	switch a.Action {
	default:
		return BadRequest("unsupported alias action: %q", a.Action)
	case "alias":
		summary = fmt.Sprintf("Enable aliases %s for snap %q", strutil.Quoted(a.Aliases), a.Snap)
		taskset, err = snapstate.Alias(state, a.Snap, a.Aliases)
	case "unalias":
		summary = fmt.Sprintf("Disable aliases %s for snap %q", strutil.Quoted(a.Aliases), a.Snap)
		taskset, err = snapstate.Unalias(state, a.Snap, a.Aliases)
	case "reset":
		summary = fmt.Sprintf("Reset aliases %s for snap %q", strutil.Quoted(a.Aliases), a.Snap)
		taskset, err = snapstate.ResetAliases(state, a.Snap, a.Aliases)
	}
	if err != nil {
		return BadRequest("%v", err)
	}

	change := state.NewChange(a.Action, summary)
	change.Set("snap-names", []string{a.Snap})
	change.AddAll(taskset)

	state.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: change.ID()})
}

type aliasStatus struct {
	App    string `json:"app,omitempty"`
	Status string `json:"status,omitempty"`
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

	allAliases, err := snapstate.Aliases(state)
	if err != nil {
		return InternalError("cannot list aliases: %v", err)
	}

	for snapName, snapst := range allStates {
		info, err := snapst.CurrentInfo()
		if err != nil {
			return InternalError("cannot retrieve info for snap %q: %v", snapName, err)
		}
		if len(info.Aliases) != 0 {
			snapAliases := make(map[string]aliasStatus)
			res[snapName] = snapAliases
			for alias, aliasApp := range info.Aliases {
				snapAliases[alias] = aliasStatus{
					App: filepath.Base(aliasApp.WrapperPath()),
				}
			}
		}
	}

	for snapName, aliasStatuses := range allAliases {
		snapAliases := res[snapName]
		if snapAliases == nil {
			snapAliases = make(map[string]aliasStatus)
			res[snapName] = snapAliases
		}
		for alias, status := range aliasStatuses {
			entry := snapAliases[alias]
			entry.Status = status
			snapAliases[alias] = entry
		}
	}

	return SyncResponse(res, nil)
}
