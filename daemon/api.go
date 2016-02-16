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
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/lockfile"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/skills"
	"github.com/ubuntu-core/snappy/snap/lightweight"
	"github.com/ubuntu-core/snappy/snappy"
)

// increase this every time you make a minor (backwards-compatible)
// change to the API.
const apiCompatLevel = "0"

var api = []*Command{
	rootCmd,
	sysInfoCmd,
	appIconCmd,
	snapsCmd,
	snapCmd,
	snapConfigCmd,
	snapSvcCmd,
	snapSvcsCmd,
	snapSvcLogsCmd,
	operationCmd,
	skillsCmd,
	assertsCmd,
	assertsFindManyCmd,
}

var (
	rootCmd = &Command{
		Path:    "/",
		GuestOK: true,
		GET:     SyncResponse([]string{"TBD"}).Self,
	}

	sysInfoCmd = &Command{
		Path:    "/2.0/system-info",
		GuestOK: true,
		GET:     sysInfo,
	}

	appIconCmd = &Command{
		Path:   "/2.0/icons/{name}.{origin}/icon",
		UserOK: true,
		GET:    appIconGet,
	}

	snapsCmd = &Command{
		Path:   "/2.0/snaps",
		UserOK: true,
		GET:    getSnapsInfo,
		POST:   sideloadSnap,
	}

	snapCmd = &Command{
		Path:   "/2.0/snaps/{name}.{origin}",
		UserOK: true,
		GET:    getSnapInfo,
		POST:   postSnap,
	}

	snapConfigCmd = &Command{
		Path: "/2.0/snaps/{name}.{origin}/config",
		GET:  snapConfig,
		PUT:  snapConfig,
	}

	snapSvcsCmd = &Command{
		Path:   "/2.0/snaps/{name}.{origin}/services",
		UserOK: true,
		GET:    snapService,
		PUT:    snapService,
	}

	snapSvcCmd = &Command{
		Path:   "/2.0/snaps/{name}.{origin}/services/{service}",
		UserOK: true,
		GET:    snapService,
		PUT:    snapService,
	}

	snapSvcLogsCmd = &Command{
		Path: "/2.0/snaps/{name}.{origin}/services/{service}/logs",
		GET:  getLogs,
	}

	operationCmd = &Command{
		Path:   "/2.0/operations/{uuid}",
		GET:    getOpInfo,
		DELETE: deleteOp,
	}

	skillsCmd = &Command{
		Path:   "/2.0/skills",
		UserOK: true,
		GET:    getSkills,
		POST:   changeSkills,
	}

	// TODO: allow to post assertions for UserOK? they are verified anyway
	assertsCmd = &Command{
		Path: "/2.0/assertions",
		POST: doAssert,
	}

	assertsFindManyCmd = &Command{
		Path:   "/2.0/assertions/{assertType}",
		UserOK: true,
		GET:    assertsFindMany,
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
		"default_channel": rel.Channel,
		"api_compat":      apiCompatLevel,
	}

	if store := snappy.StoreID(); store != "" {
		m["store"] = store
	}

	return SyncResponse(m)
}

type metarepo interface {
	Details(string, string) ([]snappy.Part, error)
	Find(string) ([]snappy.Part, error)
}

var newRemoteRepo = func() metarepo {
	return snappy.NewUbuntuStoreSnapRepository()
}

var muxVars = mux.Vars

func getSnapInfo(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	repo := newRemoteRepo()
	var part snappy.Part
	if parts, _ := repo.Details(name, origin); len(parts) > 0 {
		part = parts[0]
	}

	bag := lightweight.PartBagByName(name, origin)
	if bag == nil && part == nil {
		return NotFound("unable to find snap with name %q and origin %q", name, origin)
	}

	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError("router can't find route for snap %s.%s", name, origin)
	}

	url, err := route.URL("name", name, "origin", origin)
	if err != nil {
		return InternalError("route can't build URL for snap %s.%s: %v", name, origin, err)
	}

	result := webify(bag.Map(part), url.String())

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
		origin, _ := result["origin"].(string)
		url, err := route.URL("name", name, "origin", origin)
		if err == nil {
			result["icon"] = url.String()
		}
	}

	return result
}

type byQN []snappy.Part

func (ps byQN) Len() int      { return len(ps) }
func (ps byQN) Swap(a, b int) { ps[a], ps[b] = ps[b], ps[a] }
func (ps byQN) Less(a, b int) bool {
	return snappy.QualifiedName(ps[a]) < snappy.QualifiedName(ps[b])
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

	var bags map[string]*lightweight.PartBag

	if includeLocal {
		sources = append(sources, "local")
		bags = lightweight.AllPartBags()

		for _, v := range bags {
			m := v.Map(nil)
			name, _ := m["name"].(string)
			origin, _ := m["origin"].(string)

			resource := "no resource URL for this resource"
			url, err := route.URL("name", name, "origin", origin)
			if err == nil {
				resource = url.String()
			}

			fullname := name + "." + origin

			// strings.Contains(fullname, "") is true
			if !strings.Contains(fullname, searchTerm) {
				continue
			}

			results[fullname] = webify(m, resource)
		}
	}

	if includeStore {
		repo := newRemoteRepo()
		var found []snappy.Part

		// repo.Find("") finds all
		//
		// TODO: Instead of ignoring the error from Find:
		//   * if there are no results, return an error response.
		//   * If there are results at all (perhaps local), include a
		//     warning in the response
		found, _ = repo.Find(searchTerm)

		sources = append(sources, "store")

		sort.Sort(byQN(found))

		for _, part := range found {
			name := part.Name()
			origin := part.Origin()

			url, err := route.URL("name", name, "origin", origin)
			if err != nil {
				return InternalError("can't get route to details for %s.%s: %v", name, origin, err)
			}

			fullname := name + "." + origin
			qn := snappy.QualifiedName(part)
			results[fullname] = webify(bags[qn].Map(part), url.String())
		}
	}

	// TODO: it should be possible to search on	the "content" field on the store
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

var findServices = snappy.FindServices

type appDesc struct {
	Op     string                       `json:"op"`
	Spec   *snappy.AppYaml              `json:"spec"`
	Status *snappy.PackageServiceStatus `json:"status"`
}

func snapService(c *Command, r *http.Request) Response {
	route := c.d.router.Get(operationCmd.Path)
	if route == nil {
		return InternalError("router can't find route for operation")
	}

	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]
	if name == "" || origin == "" {
		return BadRequest("missing name or origin")
	}
	appName := vars["service"]
	pkgName := name + "." + origin

	action := "status"

	if r.Method != "GET" {
		decoder := json.NewDecoder(r.Body)
		var cmd map[string]string
		if err := decoder.Decode(&cmd); err != nil {
			return BadRequest("can't decode request body into service command: %v", err)
		}

		action = cmd["action"]
	}

	var lock lockfile.LockedFile
	reachedAsync := false
	switch action {
	case "status", "start", "stop", "restart", "enable", "disable":
		var err error
		lock, err = lockfile.Lock(dirs.SnapLockFile, true)

		if err != nil {
			return InternalError("unable to acquire lock: %v", err)
		}

		defer func() {
			if !reachedAsync {
				lock.Unlock()
			}
		}()
	default:
		return BadRequest("unknown action %s", action)
	}

	bag := lightweight.PartBagByName(name, origin)
	idx := bag.ActiveIndex()
	if idx < 0 {
		return NotFound("unable to find snap with name %q and origin %q", name, origin)
	}

	ipart, err := bag.Load(idx)
	if err != nil {
		return InternalError("unable to load active snap: %v", err)
	}

	part, ok := ipart.(*snappy.SnapPart)
	if !ok {
		return InternalError("active snap is not a *snappy.SnapPart: %T", ipart)
	}
	apps := part.Apps()

	if len(apps) == 0 {
		return NotFound("snap %q has no services", pkgName)
	}

	appmap := make(map[string]*appDesc, len(apps))
	for i := range apps {
		if apps[i].Daemon == "" {
			continue
		}
		appmap[apps[i].Name] = &appDesc{Spec: apps[i], Op: action}
	}

	if appName != "" && appmap[appName] == nil {
		return NotFound("snap %q has no service %q", pkgName, appName)
	}

	// note findServices takes the *bare* name
	actor, err := findServices(name, appName, &progress.NullProgress{})
	if err != nil {
		return InternalError("no services for %q [%q] found: %v", pkgName, appName, err)
	}

	f := func() interface{} {
		status, err := actor.ServiceStatus()
		if err != nil {
			logger.Noticef("unable to get status for %q [%q]: %v", pkgName, appName, err)
			return err
		}

		for i := range status {
			if desc, ok := appmap[status[i].ServiceName]; ok {
				desc.Status = status[i]
			} else {
				// shouldn't really happen, but can't hurt
				appmap[status[i].ServiceName] = &appDesc{Status: status[i]}
			}
		}

		if appName == "" {
			return appmap
		}

		return appmap[appName]
	}

	if action == "status" {
		return SyncResponse(f())
	}

	reachedAsync = true

	return AsyncResponse(c.d.AddTask(func() interface{} {
		defer lock.Unlock()

		switch action {
		case "start":
			err = actor.Start()
		case "stop":
			err = actor.Stop()
		case "enable":
			err = actor.Enable()
		case "disable":
			err = actor.Disable()
		case "restart":
			err = actor.Restart()
		}

		if err != nil {
			logger.Noticef("unable to %s %q [%q]: %v\n", action, pkgName, appName, err)
			return err
		}

		return f()
	}).Map(route))
}

type configurator interface {
	Configure(*snappy.SnapPart, []byte) ([]byte, error)
}

var getConfigurator = func() configurator {
	return &snappy.Overlord{}
}

func snapConfig(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]
	if name == "" || origin == "" {
		return BadRequest("missing name or origin")
	}
	pkgName := name + "." + origin

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	bag := lightweight.PartBagByName(name, origin)
	if bag == nil {
		return NotFound("no snap found with name %q and origin %q", name, origin)
	}

	idx := bag.ActiveIndex()
	if idx < 0 {
		return BadRequest("unable to configure non-active snap")
	}

	part, err := bag.Load(idx)
	if err != nil {
		return InternalError("unable to load active snap: %v", err)
	}

	bs, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return BadRequest("reading config request body gave %v", err)
	}

	overlord := getConfigurator()
	config, err := overlord.Configure(part.(*snappy.SnapPart), bs)
	if err != nil {
		return InternalError("unable to retrieve config for %s: %v", pkgName, err)
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
	LeaveOld bool         `json:"leave_old"`
	License  *licenseData `json:"license"`
	pkg      string
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

var snappyInstall = snappy.Install

func (inst *snapInstruction) install() interface{} {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}
	_, err := snappyInstall(inst.pkg, flags, inst)
	if err != nil {
		if inst.License != nil && snappy.IsLicenseNotAccepted(err) {
			return inst.License
		}
		return err
	}

	// TODO: return the log
	// also to do: commands update their output dynamically, as it changes.
	return nil
}

func (inst *snapInstruction) update() interface{} {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}

	_, err := snappy.Update(inst.pkg, flags, inst)
	return err
}

func (inst *snapInstruction) remove() interface{} {
	flags := snappy.DoRemoveGC
	if inst.LeaveOld {
		flags = 0
	}

	return snappy.Remove(inst.pkg, flags, inst)
}

func (inst *snapInstruction) purge() interface{} {
	return snappy.Purge(inst.pkg, 0, inst)
}

func (inst *snapInstruction) rollback() interface{} {
	_, err := snappy.Rollback(inst.pkg, "", inst)
	return err
}

func (inst *snapInstruction) activate() interface{} {
	return snappy.SetActive(inst.pkg, true, inst)
}

func (inst *snapInstruction) deactivate() interface{} {
	return snappy.SetActive(inst.pkg, false, inst)
}

func (inst *snapInstruction) dispatch() func() interface{} {
	switch inst.Action {
	case "install":
		return inst.install
	case "update":
		return inst.update
	case "remove":
		return inst.remove
	case "purge":
		return inst.purge
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
	inst.pkg = vars["name"] + "." + vars["origin"]

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

func newSnapImpl(filename string, origin string, unsignedOk bool) (snappy.Part, error) {
	return snappy.NewSnapFile(filename, origin, unsignedOk)
}

var newSnap = newSnapImpl

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
		defer os.Remove(tmpf.Name())

		_, err := newSnap(tmpf.Name(), snappy.SideloadedOrigin, unsignedOk)
		if err != nil {
			return err
		}

		lock, err := lockfile.Lock(dirs.SnapLockFile, true)
		if err != nil {
			return err
		}
		defer lock.Unlock()

		var flags snappy.InstallFlags
		if unsignedOk {
			flags |= snappy.AllowUnauthenticated
		}
		overlord := &snappy.Overlord{}
		name, err := overlord.Install(tmpf.Name(), snappy.SideloadedOrigin, flags, &progress.NullProgress{})
		if err != nil {
			return err
		}

		return name
	}).Map(route))
}

func getLogs(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	appName := vars["service"]

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	actor, err := findServices(name, appName, &progress.NullProgress{})
	if err != nil {
		return NotFound("no services found for %q: %v", name, err)
	}

	rawlogs, err := actor.Logs()
	if err != nil {
		return InternalError("unable to get logs for %q: %v", name, err)
	}

	logs := make([]map[string]interface{}, len(rawlogs))

	for i := range rawlogs {
		logs[i] = map[string]interface{}{
			"timestamp": rawlogs[i].Timestamp(),
			"message":   rawlogs[i].Message(),
			"raw":       rawlogs[i],
		}
	}

	return SyncResponse(logs)
}

func iconGet(name, origin string) Response {
	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError("unable to acquire lock: %v", err)
	}
	defer lock.Unlock()

	bag := lightweight.PartBagByName(name, origin)
	if bag == nil || len(bag.Versions) == 0 {
		return NotFound("unable to find snap with name %q and origin %q", name, origin)
	}

	part := bag.LoadBest()
	if part == nil {
		return NotFound("unable to load snap with name %q and origin %q", name, origin)
	}

	path := filepath.Clean(part.Icon())
	if !strings.HasPrefix(path, dirs.SnapSnapsDir) {
		// XXX: how could this happen?
		return BadRequest("requested icon is not in snap path")
	}

	return FileResponse(path)
}

func appIconGet(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]

	return iconGet(name, origin)
}

// skillGrant holds the identification of a slot that has been granted to a skill.
type skillGrant struct {
	Snap string `json:"snap"`
	Name string `json:"name"`
}

// skillInfo holds details for a skill as returned by the REST API.
type skillInfo struct {
	Snap      string       `json:"snap"`
	Name      string       `json:"name"`
	Type      string       `json:"type"`
	Label     string       `json:"label"`
	GrantedTo []skillGrant `json:"granted_to"`
}

// getSkills returns a response with a list of all the skills and which slots use them.
func getSkills(c *Command, r *http.Request) Response {
	var skills []skillInfo
	for _, skill := range c.d.skills.AllSkills("") {
		var slots []skillGrant
		for _, slot := range c.d.skills.GrantsOf(skill.Snap, skill.Name) {
			slots = append(slots, skillGrant{
				Snap: slot.Snap,
				Name: slot.Name,
			})
		}
		skills = append(skills, skillInfo{
			Snap:      skill.Snap,
			Name:      skill.Name,
			Type:      skill.Type,
			Label:     skill.Label,
			GrantedTo: slots,
		})
	}
	return SyncResponse(skills)
}

// skillAction is an action performed on the skill system.
type skillAction struct {
	Action string       `json:"action"`
	Skill  skills.Skill `json:"skill,omitempty"`
	Slot   skills.Slot  `json:"slot,omitempty"`
}

// changeSkills controls the skill system.
// Skills can be granted to and revoked from slots.
// When enableInternalSkillActions is true skills and slots can also be
// explicitly added and removed.
func changeSkills(c *Command, r *http.Request) Response {
	var a skillAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&a); err != nil {
		return BadRequest("cannot decode request body into a skill action: %v", err)
	}
	if a.Action == "" {
		return BadRequest("skill action not specified")
	}
	if !c.d.enableInternalSkillActions && a.Action != "grant" && a.Action != "revoke" {
		return BadRequest("internal skill actions are disabled")
	}
	switch a.Action {
	case "grant":
		err := c.d.skills.Grant(a.Skill.Snap, a.Skill.Name, a.Slot.Snap, a.Slot.Name)
		if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(nil)
	case "revoke":
		err := c.d.skills.Revoke(a.Skill.Snap, a.Skill.Name, a.Slot.Snap, a.Slot.Name)
		if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(nil)
	case "add-skill":
		err := c.d.skills.AddSkill(&a.Skill)
		if err != nil {
			return BadRequest("%v", err)
		}
		return &resp{
			Type:   ResponseTypeSync,
			Status: http.StatusCreated,
		}
	case "remove-skill":
		err := c.d.skills.RemoveSkill(a.Skill.Snap, a.Skill.Name)
		if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(nil)
	case "add-slot":
		err := c.d.skills.AddSlot(&a.Slot)
		if err != nil {
			return BadRequest("%v", err)
		}
		return &resp{
			Type:   ResponseTypeSync,
			Status: http.StatusCreated,
		}
	case "remove-slot":
		err := c.d.skills.RemoveSlot(a.Slot.Snap, a.Slot.Name)
		if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(nil)
	}
	return BadRequest("unsupported skill action: %q", a.Action)
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
	if err := c.d.asserts.Add(a); err != nil {
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
	assertions, err := c.d.asserts.FindMany(assertType, headers)
	if err == asserts.ErrNotFound {
		return AssertResponse(nil, true)
	} else if err != nil {
		return InternalError("searching assertions failed: %v", err)
	}
	return AssertResponse(assertions, true)
}
