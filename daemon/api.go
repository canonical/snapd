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
	"github.com/ubuntu-core/snappy/lockfile"
	"github.com/ubuntu-core/snappy/overlord"
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
	operationCmd,
	interfacesCmd,
	assertsCmd,
	assertsFindManyCmd,
	eventsCmd,
	stateChangesCmd,
}

var (
	rootCmd = &Command{
		Path:    "/",
		GuestOK: true,
		GET:     SyncResponse([]string{"TBD"}).Self,
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
	operationCmd = &Command{
		Path:   "/v2/operations/{uuid}",
		GET:    getOpInfo,
		DELETE: deleteOp,
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

	eventsCmd = &Command{
		Path: "/v2/events",
		GET:  getEvents,
	}

	stateChangesCmd = &Command{
		Path:   "/v2/changes",
		UserOK: true,
		GET:    getChanges,
	}
)

func sysInfo(c *Command, r *http.Request) Response {
	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

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

	return SyncResponse(m)
}

type authState struct {
	Users []userAuthState `json:"users"`
}

type userAuthState struct {
	Username   string   `json:"username,omitempty"`
	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
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
		return InternalError("cannot get package access macaroon")
	}

	discharge, err := store.DischargeAuthCaveat(loginData.Username, loginData.Password, macaroon, loginData.Otp)
	if err == store.ErrAuthenticationNeeds2fa {
		twofactorRequiredResponse := &resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Kind:    errorKindTwoFactorRequired,
				Message: "two factor authentication required",
			},
			Status: http.StatusUnauthorized,
		}
		return SyncResponse(twofactorRequiredResponse)
	}
	if err != nil {
		return Unauthorized("cannot get discharge authorization")
	}

	authenticatedUser := userAuthState{
		Username:   loginData.Username,
		Macaroon:   macaroon,
		Discharges: []string{discharge},
	}
	// TODO Handle better the multi-user case.
	authStateData := authState{Users: []userAuthState{authenticatedUser}}

	overlord := c.d.overlord
	state := overlord.State()
	state.Lock()
	state.Set("auth", authStateData)
	state.Unlock()

	result := loginResponseData{
		Macaroon:   macaroon,
		Discharges: []string{discharge},
	}
	return SyncResponse(result)
}

type metarepo interface {
	Snap(string, string) (*snap.Info, error)
	FindSnaps(string, string) ([]*snap.Info, error)
}

var newRemoteRepo = func() metarepo {
	return snappy.NewConfiguredUbuntuStoreSnapRepository()
}

var muxVars = mux.Vars

func getSnapInfo(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	channel := ""
	remoteSnap, _ := newRemoteRepo().Snap(name, channel)

	installed, err := (&snappy.Overlord{}).Installed()
	if err != nil {
		return InternalError("cannot load snaps: %v", err)
	}
	localSnaps := snappy.FindSnapsByName(name, installed)

	if len(localSnaps) == 0 && remoteSnap == nil {
		return NotFound("unable to find snap with name %q", name)
	}

	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError("router can't find route for snap %s", name)
	}

	url, err := route.URL("name", name)
	if err != nil {
		return InternalError("route can't build URL for snap %s: %v", name, err)
	}

	result := webify(mapSnap(localSnaps, remoteSnap), url.String())

	return SyncResponse(result)
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

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	// TODO: Marshal incrementally leveraging json.RawMessage.
	results := make(map[string]map[string]interface{})
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

	var localSnapsMap map[string][]*snappy.Snap
	var remoteSnapMap map[string]*snap.Info

	if includeLocal {
		sources = append(sources, "local")
		localSnapsMap, _ = allSnaps()
	}

	if includeStore {
		remoteSnapMap = make(map[string]*snap.Info)

		// repo.Find("") finds all
		//
		// TODO: Instead of ignoring the error from Find:
		//   * if there are no results, return an error response.
		//   * If there are results at all (perhaps local), include a
		//     warning in the response
		found, _ := newRemoteRepo().FindSnaps(searchTerm, "")

		sources = append(sources, "store")

		for _, snap := range found {
			remoteSnapMap[snap.Name()] = snap
		}
	}

	for name, localSnaps := range localSnapsMap {
		// strings.Contains(fullname, "") is true
		if !strings.Contains(name, searchTerm) {
			continue
		}

		m := mapSnap(localSnaps, remoteSnapMap[name])
		resource := "no resource URL for this resource"
		url, err := route.URL("name", name)
		if err == nil {
			resource = url.String()
		}

		results[name] = webify(m, resource)
	}

	for name, remoteSnap := range remoteSnapMap {
		if _, ok := results[name]; ok {
			// already done
			continue
		}

		m := mapSnap(nil, remoteSnap)

		resource := "no resource URL for this resource"
		url, err := route.URL("name", remoteSnap.Name())
		if err == nil {
			resource = url.String()
		}

		results[name] = webify(m, resource)
	}

	// TODO: it should be possible to search on the "content" field on the store
	//       with multiple values, see:
	//       https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#Search
	if len(includeTypes) > 0 {
		for name, result := range results {
			if !resultHasType(result, includeTypes) {
				delete(results, name)
			}
		}
	}

	return SyncResponse(map[string]interface{}{
		"snaps":   results,
		"sources": sources,
		"paging": map[string]interface{}{
			"pages": 1,
			"page":  1,
			"count": len(results),
		},
	})
}

func resultHasType(r map[string]interface{}, allowedTypes []string) bool {
	for _, t := range allowedTypes {
		if r["type"] == t {
			return true
		}
	}
	return false
}

type appDesc struct {
	Op   string          `json:"op"`
	Spec *snappy.AppYaml `json:"spec"`
}

type configurator interface {
	Configure(*snappy.Snap, []byte) ([]byte, error)
}

var getConfigurator = func() configurator {
	return &snappy.Overlord{}
}

func snapConfig(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	snapName := vars["name"]

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	installed, err := (&snappy.Overlord{}).Installed()
	snaps := snappy.FindSnapsByName(snapName, installed)
	_, part := bestSnap(snaps)
	if err != nil || part == nil {
		return NotFound("no snap found with name %q", snapName)
	}

	if !part.IsActive() {
		return BadRequest("unable to configure non-active snap")
	}

	bs, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return BadRequest("reading config request body gave %v", err)
	}

	overlord := getConfigurator()
	config, err := overlord.Configure(part, bs)
	if err != nil {
		return InternalError("unable to retrieve config for %s: %v", snapName, err)
	}

	return SyncResponse(string(config))
}

func getOpInfo(c *Command, r *http.Request) Response {
	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError("router can't find route for operation")
	}

	id := muxVars(r)["uuid"]
	task := c.d.GetTask(id)
	if task == nil {
		return NotFound("unable to find task with id %q", id)
	}

	return SyncResponse(task.Map(route))
}

func deleteOp(c *Command, r *http.Request) Response {
	id := muxVars(r)["uuid"]
	err := c.d.DeleteTask(id)

	switch err {
	case nil:
		return SyncResponse("done")
	case errTaskNotFound:
		return NotFound("unable to find task %q", id)
	case errTaskStillRunning:
		return BadRequest("unable to delete task %q: still running", id)
	default:
		return InternalError("unable to delete task %q: %v", id, err)
	}
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

func (inst *snapInstruction) install() interface{} {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}
	state := inst.overlord.State()
	state.Lock()
	msg := fmt.Sprintf(i18n.G("Install %q snap"), inst.pkg)
	if inst.Channel != "stable" {
		msg = fmt.Sprintf(i18n.G("Install %q snap from %q channel"), inst.pkg, inst.Channel)
	}
	chg := state.NewChange("install-snap", msg)
	ts, err := snapstateInstall(state, inst.pkg, inst.Channel, flags)
	if err == nil {
		chg.AddAll(ts)
	}
	state.Unlock()
	if err != nil {
		return err
	}
	state.EnsureBefore(0)
	err = waitChange(chg)
	return err
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

func (inst *snapInstruction) update() interface{} {
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
		return err
	}

	state.EnsureBefore(0)
	return waitChange(chg)
}

func (inst *snapInstruction) remove() interface{} {
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
		return err
	}

	state.EnsureBefore(0)
	return waitChange(chg)
}

func (inst *snapInstruction) rollback() interface{} {
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
		return err
	}

	state.EnsureBefore(0)
	return waitChange(chg)
}

func (inst *snapInstruction) activate() interface{} {
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
		return err
	}

	state.EnsureBefore(0)
	return waitChange(chg)
}

func (inst *snapInstruction) deactivate() interface{} {
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
		return err
	}

	state.EnsureBefore(0)
	return waitChange(chg)
}

func (inst *snapInstruction) dispatch() func() interface{} {
	switch inst.Action {
	case "install":
		return inst.install
	case "update":
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

func pkgActionDispatchImpl(inst *snapInstruction) func() interface{} {
	return inst.dispatch()
}

var pkgActionDispatch = pkgActionDispatchImpl

func postSnap(c *Command, r *http.Request) Response {
	route := c.d.router.Get(operationCmd.Path)
	if route == nil {
		return InternalError("router can't find route for operation")
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

	return AsyncResponse(c.d.AddTask(func() interface{} {
		lock, err := lockfile.Lock(dirs.SnapLockFile, true)
		if err != nil {
			return err
		}
		defer lock.Unlock()
		return f()
	}).Map(route))
}

const maxReadBuflen = 1024 * 1024

func sideloadSnap(c *Command, r *http.Request) Response {
	route := c.d.router.Get(operationCmd.Path)
	if route == nil {
		return InternalError("router can't find route for operation")
	}

	body := r.Body
	unsignedOk := false
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/") {
		// spec says POSTs to sideload snaps should be “a multipart file upload”

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

	return AsyncResponse(c.d.AddTask(func() interface{} {
		lock, err := lockfile.Lock(dirs.SnapLockFile, true)
		if err != nil {
			return err
		}
		defer lock.Unlock()

		var flags snappy.InstallFlags
		if unsignedOk {
			flags |= snappy.AllowUnauthenticated
		}

		snap := tmpf.Name()
		defer os.Remove(snap)

		state := c.d.overlord.State()
		state.Lock()
		msg := fmt.Sprintf(i18n.G("Install local %q snap"), snap)
		chg := state.NewChange("install-snap", msg)
		ts, err := snapstateInstall(state, snap, "", flags)
		if err == nil {
			chg.AddAll(ts)
		}
		state.Unlock()
		if err != nil {
			return err
		}
		state.EnsureBefore(0)
		return waitChange(chg)
	}).Map(route))
}

func iconGet(name string) Response {
	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	installed, err := (&snappy.Overlord{}).Installed()
	snaps := snappy.FindSnapsByName(name, installed)
	_, snap := bestSnap(snaps)
	if err != nil || snap == nil {
		return NotFound("unable to find snap with name %q", name)
	}

	path := filepath.Clean(snap.Icon())
	if !strings.HasPrefix(path, dirs.SnapSnapsDir) {
		// XXX: how could this happen?
		return BadRequest("requested icon is not in snap path")
	}

	return FileResponse(path)
}

func appIconGet(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]

	return iconGet(name)
}

// getInterfaces returns all plugs and slots.
func getInterfaces(c *Command, r *http.Request) Response {
	return SyncResponse(c.d.interfaces.Interfaces())
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
	switch a.Action {
	case "connect":
		if len(a.Plugs) == 0 || len(a.Slots) == 0 {
			return BadRequest("at least one plug and slot is required")
		}
		err := c.d.interfaces.Connect(a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(nil)
	case "disconnect":
		if len(a.Plugs) == 0 || len(a.Slots) == 0 {
			return BadRequest("at least one plug and slot is required")
		}
		err := c.d.interfaces.Disconnect(a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(nil)
	}
	return BadRequest("unsupported interface action: %q", a.Action)
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
	Kind    string      `json:"kind"`
	Summary string      `json:"summary"`
	Status  string      `json:"status"`
	Tasks   []*taskInfo `json:"tasks,omitempty"`
}

type taskInfo struct {
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Status  string   `json:"status"`
	Log     []string `json:"log,omitempty"`
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
		chgInfo := &changeInfo{
			Kind:    chg.Kind(),
			Summary: chg.Summary(),
			Status:  chg.Status().String(),
		}
		tasks := chg.Tasks()
		taskInfos := make([]*taskInfo, len(tasks))
		for j, t := range tasks {
			taskInfo := &taskInfo{
				Kind:    t.Kind(),
				Summary: t.Summary(),
				Status:  t.Status().String(),
				Log:     t.Log(),
			}
			taskInfos[j] = taskInfo
		}
		chgInfo.Tasks = taskInfos
		chgInfos = append(chgInfos, chgInfo)
	}
	return SyncResponse(chgInfos)
}
