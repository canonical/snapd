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
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/overlord"
	"github.com/ubuntu-core/snappy/overlord/auth"
	"github.com/ubuntu-core/snappy/overlord/ifacestate"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
	"github.com/ubuntu-core/snappy/store"
)

// increase this every time you make a minor (backwards-compatible)
// change to the API.
const apiCompatLevel = "0"

var api = []*Command{
	rootCmd,
	sysInfoCmd,
	loginCmd,
	appIconCmd,
	snapsCmd,
	snapCmd,
	//FIXME: renenable config for GA
	//snapConfigCmd,
	interfacesCmd,
	assertsCmd,
	assertsFindManyCmd,
	eventsCmd,
	stateChangeCmd,
	stateChangesCmd,
}

var (
	rootCmd = &Command{
		Path:    "/",
		GuestOK: true,
		GET:     SyncResponse([]string{"TBD"}, nil).Self,
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

	appIconCmd = &Command{
		Path:   "/v2/icons/{name}/icon",
		UserOK: true,
		GET:    appIconGet,
	}

	snapsCmd = &Command{
		Path:   "/v2/snaps",
		UserOK: true,
		GET:    getSnapsInfo,
		POST:   sideloadSnap,
	}

	snapCmd = &Command{
		Path:   "/v2/snaps/{name}",
		UserOK: true,
		GET:    getSnapInfo,
		POST:   postSnap,
	}
	//FIXME: renenable config for GA
	/*
		snapConfigCmd = &Command{
			Path: "/v2/snaps/{name}/config",
			GET:  snapConfig,
			PUT:  snapConfig,
		}
	*/

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

	eventsCmd = &Command{
		Path: "/v2/events",
		GET:  getEvents,
	}

	stateChangeCmd = &Command{
		Path:   "/v2/changes/{id}",
		UserOK: true,
		GET:    getChange,
	}

	stateChangesCmd = &Command{
		Path:   "/v2/changes",
		UserOK: true,
		GET:    getChanges,
	}
)

func sysInfo(c *Command, r *http.Request) Response {
	rel := release.Get()
	m := map[string]string{
		"flavor":          rel.Flavor,
		"release":         rel.Series,
		"default-channel": rel.Channel,
		"api-compat":      apiCompatLevel,
	}

	if store := snappy.StoreID(); store != "" {
		m["store"] = store
	}

	return SyncResponse(m, nil)
}

type loginResponseData struct {
	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

func loginUser(c *Command, r *http.Request) Response {
	var loginData struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Otp      string `json:"otp"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&loginData); err != nil {
		return BadRequest("cannot decode login data from request body: %v", err)
	}

	macaroon, err := store.RequestPackageAccessMacaroon()
	if err != nil {
		return InternalError(err.Error())
	}

	discharge, err := store.DischargeAuthCaveat(loginData.Username, loginData.Password, macaroon, loginData.Otp)
	if err == store.ErrAuthenticationNeeds2fa {
		twofactorRequiredResponse := &resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Kind:    errorKindTwoFactorRequired,
				Message: store.ErrAuthenticationNeeds2fa.Error(),
			},
			Status: http.StatusUnauthorized,
		}
		return SyncResponse(twofactorRequiredResponse, nil)
	}
	if err != nil {
		return Unauthorized(err.Error())
	}

	overlord := c.d.overlord
	state := overlord.State()
	state.Lock()
	_, err = auth.NewUser(state, loginData.Username, macaroon, []string{discharge})
	state.Unlock()
	if err != nil {
		return InternalError("cannot persist authentication details: %v", err)
	}

	result := loginResponseData{
		Macaroon:   macaroon,
		Discharges: []string{discharge},
	}
	return SyncResponse(result, nil)
}

// UserFromRequest extracts user information from request and return the respective user in state, if valid
// It requires the state to be locked
func UserFromRequest(st *state.State, req *http.Request) (*auth.UserState, error) {
	// extract macaroons data from request
	header := req.Header.Get("Authorization")
	if header == "" {
		return nil, errNoAuth
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

	if macaroon == "" || len(discharges) == 0 {
		return nil, fmt.Errorf("invalid authorization header")
	}

	user, err := auth.CheckMacaroon(st, macaroon, discharges)
	return user, err
}

type metarepo interface {
	Snap(string, string, store.Authenticator) (*snap.Info, error)
	FindSnaps(string, string, store.Authenticator) ([]*snap.Info, error)
	SuggestedCurrency() string
}

var newRemoteRepo = func() metarepo {
	return snappy.NewConfiguredUbuntuStoreSnapRepository()
}

var muxVars = mux.Vars

func getSnapInfo(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]

	channel := ""
	remoteRepo := newRemoteRepo()
	suggestedCurrency := remoteRepo.SuggestedCurrency()

	localSnap, active, err := localSnapInfo(c.d.overlord.State(), name)
	if err != nil {
		return InternalError("%v", err)
	}

	if localSnap != nil {
		channel = localSnap.Channel
	}

	auther, err := c.d.auther(r)
	if err != nil && err != errNoAuth {
		return InternalError("%v", err)
	}

	remoteSnap, _ := remoteRepo.Snap(name, channel, auther)

	if localSnap == nil && remoteSnap == nil {
		return NotFound("cannot find snap %q", name)
	}

	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError("router can't find route for snap %s", name)
	}

	url, err := route.URL("name", name)
	if err != nil {
		return InternalError("route can't build URL for snap %s: %v", name, err)
	}

	result := webify(mapSnap(localSnap, active, remoteSnap), url.String())

	meta := &Meta{
		SuggestedCurrency: suggestedCurrency,
	}
	return SyncResponse(result, meta)
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

// plural!
func getSnapsInfo(c *Command, r *http.Request) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("router can't find route for snaps")
	}

	sources := make([]string, 0, 2)
	query := r.URL.Query()

	var includeStore, includeLocal bool
	if len(query["sources"]) > 0 {
		for _, v := range strings.Split(query["sources"][0], ",") {
			if v == "store" {
				includeStore = true
			} else if v == "local" {
				includeLocal = true
			}
		}
	} else {
		includeStore = true
		includeLocal = true
	}

	searchTerm := query.Get("q")

	var includeTypes []string
	if len(query["types"]) > 0 {
		includeTypes = strings.Split(query["types"][0], ",")
	}

	var aboutSnaps []aboutSnap
	var remoteSnapMap map[string]*snap.Info

	if includeLocal {
		sources = append(sources, "local")
		aboutSnaps, _ = allLocalSnapInfos(c.d.overlord.State())
	}

	var suggestedCurrency string

	if includeStore {
		remoteSnapMap = make(map[string]*snap.Info)

		remoteRepo := newRemoteRepo()

		auther, err := c.d.auther(r)
		if err != nil && err != errNoAuth {
			return InternalError("%v", err)
		}

		// repo.Find("") finds all
		//
		// TODO: Instead of ignoring the error from Find:
		//   * if there are no results, return an error response.
		//   * If there are results at all (perhaps local), include a
		//     warning in the response
		found, _ := remoteRepo.FindSnaps(searchTerm, "", auther)
		suggestedCurrency = remoteRepo.SuggestedCurrency()

		sources = append(sources, "store")

		for _, snap := range found {
			remoteSnapMap[snap.Name()] = snap
		}
	}

	seen := make(map[string]bool)
	results := make([]*json.RawMessage, 0, len(aboutSnaps)+len(remoteSnapMap))

	addResult := func(name string, m map[string]interface{}) {
		if seen[name] {
			return
		}
		seen[name] = true

		// TODO Search the store for "content" with multiple values. See:
		//      https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#Search
		if len(includeTypes) > 0 && !resultHasType(m, includeTypes) {
			return
		}

		resource := ""
		url, err := route.URL("name", name)
		if err == nil {
			resource = url.String()
		}

		data, err := json.Marshal(webify(m, resource))
		if err != nil {
			return
		}
		raw := json.RawMessage(data)
		results = append(results, &raw)
	}

	for _, about := range aboutSnaps {
		info := about.info
		name := info.Name()
		// strings.Contains(name, "") is true
		if strings.Contains(name, searchTerm) {
			active := about.snapst.Active
			addResult(name, mapSnap(info, active, remoteSnapMap[name]))
		}
	}

	for name, remoteSnap := range remoteSnapMap {
		addResult(name, mapSnap(nil, false, remoteSnap))
	}

	meta := &Meta{
		Sources: sources,
		Paging: &Paging{
			Page:  1,
			Pages: 1,
		},
		SuggestedCurrency: suggestedCurrency,
	}
	return SyncResponse(results, meta)
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
	Action   string       `json:"action"`
	Channel  string       `json:"channel"`
	LeaveOld bool         `json:"leave-old"`
	License  *licenseData `json:"license"`
	pkg      string

	overlord *overlord.Overlord
}

// Agreed is part of the progress.Meter interface (q.v.)
// ask the user whether they agree to the given license's text
func (inst *snapInstruction) Agreed(intro, license string) bool {
	if inst.License == nil || !inst.License.Agreed || inst.License.Intro != intro || inst.License.License != license {
		inst.License = &licenseData{Intro: intro, License: license, Agreed: false}
		return false
	}

	return true
}

var snapstateInstall = snapstate.Install
var snapstateInstallPath = snapstate.InstallPath
var snapstateGet = snapstate.Get

func waitChange(chg *state.Change) error {
	select {
	case <-chg.Ready():
	}
	// TODO case <-daemon.Dying():
	st := chg.State()
	st.Lock()
	defer st.Unlock()
	return chg.Err()
}

func ensureUbuntuCore(chg *state.Change) error {
	var ss snapstate.SnapState

	ubuntuCore := "ubuntu-core"
	err := snapstateGet(chg.State(), ubuntuCore, &ss)
	if err != state.ErrNoState {
		return err
	}

	// FIXME: workaround because we are not fully state based yet
	installed, err := (&snappy.Overlord{}).Installed()
	snaps := snappy.FindSnapsByName(ubuntuCore, installed)
	if len(snaps) > 0 {
		return nil
	}

	return installSnap(chg, ubuntuCore, "stable", 0)
}

func installSnap(chg *state.Change, name, channel string, flags snappy.InstallFlags) error {
	st := chg.State()
	ts, err := snapstateInstall(st, name, channel, flags)
	if err != nil {
		return err
	}

	// ensure that each of our task runs after the existing tasks
	chgts := state.NewTaskSet(chg.Tasks()...)
	for _, t := range ts.Tasks() {
		t.WaitAll(chgts)
	}
	chg.AddAll(ts)

	return nil
}

func (inst *snapInstruction) install() (*state.Change, error) {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}
	msg := fmt.Sprintf(i18n.G("Install %q snap"), inst.pkg)
	if inst.Channel != "stable" {
		msg = fmt.Sprintf(i18n.G("Install %q snap from %q channel"), inst.pkg, inst.Channel)
	}

	st := inst.overlord.State()
	st.Lock()
	chg := st.NewChange("install-snap", msg)
	err := ensureUbuntuCore(chg)
	if err == nil {
		err = installSnap(chg, inst.pkg, inst.Channel, flags)
	}
	st.Unlock()
	if err != nil {
		return nil, err
	}

	st.EnsureBefore(0)

	return chg, nil

	// FIXME: handle license agreement need to happen in the above
	//        code
	/*
		_, err := snappyInstall(inst.pkg, inst.Channel, flags, inst)
		if err != nil {
			if inst.License != nil && snappy.IsLicenseNotAccepted(err) {
				return inst.License
			}
			return err
		}
	*/
}

func (inst *snapInstruction) update() (*state.Change, error) {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}
	state := inst.overlord.State()
	state.Lock()
	msg := fmt.Sprintf(i18n.G("Update %q snap"), inst.pkg)
	if inst.Channel != "stable" {
		msg = fmt.Sprintf(i18n.G("Update %q snap from %q channel"), inst.pkg, inst.Channel)
	}
	chg := state.NewChange("update-snap", msg)
	ts, err := snapstate.Update(state, inst.pkg, inst.Channel, flags)
	if err == nil {
		chg.AddAll(ts)
	}
	state.Unlock()
	if err != nil {
		return nil, err
	}

	state.EnsureBefore(0)

	return chg, nil
}

func (inst *snapInstruction) remove() (*state.Change, error) {
	flags := snappy.DoRemoveGC
	if inst.LeaveOld {
		flags = 0
	}
	state := inst.overlord.State()
	state.Lock()
	msg := fmt.Sprintf(i18n.G("Remove %q snap"), inst.pkg)
	chg := state.NewChange("remove-snap", msg)
	ts, err := snapstate.Remove(state, inst.pkg, flags)
	if err == nil {
		chg.AddAll(ts)
	}
	state.Unlock()
	if err != nil {
		return nil, err
	}

	state.EnsureBefore(0)

	return chg, nil
}

func (inst *snapInstruction) rollback() (*state.Change, error) {
	state := inst.overlord.State()
	state.Lock()
	msg := fmt.Sprintf(i18n.G("Rollback %q snap"), inst.pkg)
	chg := state.NewChange("rollback-snap", msg)
	// use previous version
	ver := ""
	ts, err := snapstate.Rollback(state, inst.pkg, ver)
	if err == nil {
		chg.AddAll(ts)
	}
	state.Unlock()
	if err != nil {
		return nil, err
	}

	state.EnsureBefore(0)

	return chg, nil
}

func (inst *snapInstruction) activate() (*state.Change, error) {
	state := inst.overlord.State()
	state.Lock()
	msg := fmt.Sprintf(i18n.G("Activate %q snap"), inst.pkg)
	chg := state.NewChange("activate-snap", msg)
	ts, err := snapstate.Activate(state, inst.pkg)
	if err == nil {
		chg.AddAll(ts)
	}
	state.Unlock()
	if err != nil {
		return nil, err
	}

	state.EnsureBefore(0)

	return chg, nil
}

func (inst *snapInstruction) deactivate() (*state.Change, error) {
	state := inst.overlord.State()
	state.Lock()
	msg := fmt.Sprintf(i18n.G("Deactivate %q snap"), inst.pkg)
	chg := state.NewChange("deactivate-snap", msg)
	ts, err := snapstate.Deactivate(state, inst.pkg)
	if err == nil {
		chg.AddAll(ts)
	}
	state.Unlock()
	if err != nil {
		return nil, err
	}

	state.EnsureBefore(0)

	return chg, nil
}

func (inst *snapInstruction) dispatch() func() (*state.Change, error) {
	switch inst.Action {
	case "install":
		return inst.install
	case "refresh":
		return inst.update
	case "remove":
		return inst.remove
	case "rollback":
		return inst.rollback
	case "activate":
		return inst.activate
	case "deactivate":
		return inst.deactivate
	default:
		return nil
	}
}

func pkgActionDispatchImpl(inst *snapInstruction) func() (*state.Change, error) {
	return inst.dispatch()
}

var pkgActionDispatch = pkgActionDispatchImpl

func postSnap(c *Command, r *http.Request) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("router can't find route for change")
	}

	decoder := json.NewDecoder(r.Body)
	var inst snapInstruction
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("can't decode request body into snap instruction: %v", err)
	}

	vars := muxVars(r)
	inst.pkg = vars["name"]
	inst.overlord = c.d.overlord

	f := pkgActionDispatch(&inst)
	if f == nil {
		return BadRequest("unknown action %s", inst.Action)
	}

	chg, err := f()
	if err != nil {
		return InternalError("can't %s %q: %v", inst.Action, inst.pkg, err)
	}

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

const maxReadBuflen = 1024 * 1024

func sideloadSnap(c *Command, r *http.Request) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("router can't find route for change")
	}

	body := r.Body
	unsignedOk := false
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/") {
		// spec says POSTs to sideload snaps should be "a multipart file upload"

		_, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			return BadRequest("unable to parse POST body: %v", err)
		}

		form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(maxReadBuflen)
		if err != nil {
			return BadRequest("unable to read POST form: %v", err)
		}

		// if allow-unsigned is present in the form, unsigned is OK
		_, unsignedOk = form.Value["allow-unsigned"]

		// form.File is a map of arrays of *FileHeader things
		// we just allow one (for now at least)
	out:
		for _, v := range form.File {
			for i := range v {
				body, err = v[i].Open()
				if err != nil {
					return BadRequest("unable to open POST form file: %v", err)
				}
				defer body.Close()

				break out
			}
		}
		defer form.RemoveAll()
	} else {
		// Looks like user didn't understand that multipart thing.
		// Maybe they just POSTed the snap at us (quite handy to do with e.g. curl).
		// So we try that.

		// If x-allow-unsigned is present, unsigned is OK
		_, unsignedOk = r.Header["X-Allow-Unsigned"]
	}

	tmpf, err := ioutil.TempFile("", "snapd-sideload-pkg-")
	if err != nil {
		return InternalError("can't create tempfile: %v", err)
	}

	if _, err := io.Copy(tmpf, body); err != nil {
		os.Remove(tmpf.Name())
		return InternalError("can't copy request into tempfile: %v", err)
	}

	var flags snappy.InstallFlags
	if unsignedOk {
		flags |= snappy.AllowUnauthenticated
	}

	snap := tmpf.Name()

	state := c.d.overlord.State()
	state.Lock()
	msg := fmt.Sprintf(i18n.G("Install local %q snap"), snap)
	chg := state.NewChange("install-snap", msg)

	err = ensureUbuntuCore(chg)
	if err == nil {
		ts, err := snapstateInstallPath(state, snap, "", flags)
		if err == nil {
			chg.AddAll(ts)
		}
	}
	state.Unlock()
	go func() {
		// XXX this needs to be a task in the manager; this is a hack to keep this branch smaller
		<-chg.Ready()
		os.Remove(snap)
	}()
	if err != nil {
		return InternalError("can't request sideload: %v", err)
	}
	state.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func iconGet(st *state.State, name string) Response {
	info, _, err := localSnapInfo(st, name)
	if err != nil {
		return InternalError("%v", err)
	}
	if info == nil {
		return NotFound("cannot find snap %q", name)
	}

	path := filepath.Clean(snapIcon(info))
	if !strings.HasPrefix(path, dirs.SnapSnapsDir) {
		// XXX: how could this happen?
		return BadRequest("requested icon is not in snap path")
	}

	return FileResponse(path)
}

func appIconGet(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]

	return iconGet(c.d.overlord.State(), name)
}

// getInterfaces returns all plugs and slots.
func getInterfaces(c *Command, r *http.Request) Response {
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
func changeInterfaces(c *Command, r *http.Request) Response {
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

	var change *state.Change
	var taskset *state.TaskSet
	var err error

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	switch a.Action {
	case "connect":
		summary := fmt.Sprintf("Connect %s:%s to %s:%s", a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		change = state.NewChange("connect-snap", summary)
		taskset, err = ifacestate.Connect(state, a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
	case "disconnect":
		summary := fmt.Sprintf("Disconnect %s:%s from %s:%s", a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		change = state.NewChange("disconnect-snap", summary)
		taskset, err = ifacestate.Disconnect(state, a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
	}

	if err == nil {
		change.AddAll(taskset)
	}

	if err != nil {
		return BadRequest("%v", err)
	}

	state.EnsureBefore(0)

	return AsyncResponse(nil, &Meta{Change: change.ID()})
}

func doAssert(c *Command, r *http.Request) Response {
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return BadRequest("reading assert request body gave %v", err)
	}
	a, err := asserts.Decode(b)
	if err != nil {
		return BadRequest("can't decode request body into an assertion: %v", err)
	}
	// TODO/XXX: turn this into a Change/Task combination
	amgr := c.d.overlord.AssertManager()
	if err := amgr.DB().Add(a); err != nil {
		// TODO: have a specific error to be able to return  409 for not newer revision?
		return BadRequest("assert failed: %v", err)
	}
	// TODO: what more info do we want to return on success?
	return &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
	}
}

func assertsFindMany(c *Command, r *http.Request) Response {
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
	amgr := c.d.overlord.AssertManager()
	assertions, err := amgr.DB().FindMany(assertType, headers)
	if err == asserts.ErrNotFound {
		return AssertResponse(nil, true)
	} else if err != nil {
		return InternalError("searching assertions failed: %v", err)
	}
	return AssertResponse(assertions, true)
}

func getEvents(c *Command, r *http.Request) Response {
	return EventResponse(c.d.hub)
}

type changeInfo struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Summary string      `json:"summary"`
	Status  string      `json:"status"`
	Tasks   []*taskInfo `json:"tasks,omitempty"`
	Ready   bool        `json:"ready"`
	Err     string      `json:"err,omitempty"`
}

type taskInfo struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Summary  string           `json:"summary"`
	Status   string           `json:"status"`
	Log      []string         `json:"log,omitempty"`
	Progress taskInfoProgress `json:"progress"`
}

type taskInfoProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

func change2changeInfo(chg *state.Change) *changeInfo {
	status := chg.Status()
	chgInfo := &changeInfo{
		ID:      chg.ID(),
		Kind:    chg.Kind(),
		Summary: chg.Summary(),
		Status:  status.String(),
		Ready:   status.Ready(),
	}
	if err := chg.Err(); err != nil {
		chgInfo.Err = err.Error()
	}

	tasks := chg.Tasks()
	taskInfos := make([]*taskInfo, len(tasks))
	for j, t := range tasks {
		done, total := t.Progress()
		taskInfo := &taskInfo{
			ID:      t.ID(),
			Kind:    t.Kind(),
			Summary: t.Summary(),
			Status:  t.Status().String(),
			Log:     t.Log(),
			Progress: taskInfoProgress{
				Done:  done,
				Total: total,
			},
		}
		taskInfos[j] = taskInfo
	}
	chgInfo.Tasks = taskInfos

	return chgInfo
}

func getChange(c *Command, r *http.Request) Response {
	chID := muxVars(r)["id"]
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chg := state.Change(chID)
	if chg == nil {
		return NotFound("unable to find change with id %q", chID)
	}

	return SyncResponse(change2changeInfo(chg), nil)
}

func getChanges(c *Command, r *http.Request) Response {
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
