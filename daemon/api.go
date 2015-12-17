// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"sort"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ubuntu-core/snappy/caps"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/lockfile"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/pkg/lightweight"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snappy"
)

// increase this every time you make a minor (backwards-compatible)
// change to the API.
const apiCompatLevel = "1"

var api = []*Command{
	rootCmd,
	v1Cmd,
	appIconCmd,
	packagesCmd,
	packageCmd,
	packageConfigCmd,
	packageSvcCmd,
	packageSvcsCmd,
	packageSvcLogsCmd,
	operationCmd,
	capabilitiesCmd,
	capabilityCmd,
}

var (
	rootCmd = &Command{
		Path:    "/",
		GuestOK: true,
		GET:     SyncResponse([]string{"/1.0"}).Self,
	}

	v1Cmd = &Command{
		Path:    "/1.0",
		GuestOK: true,
		GET:     v1Get,
	}

	appIconCmd = &Command{
		Path:   "/1.0/icons/{name}.{origin}/icon",
		UserOK: true,
		GET:    appIconGet,
	}

	packagesCmd = &Command{
		Path:   "/1.0/packages",
		UserOK: true,
		GET:    getPackagesInfo,
		POST:   sideloadPackage,
		PUT:    configMulti,
	}

	packageCmd = &Command{
		Path:   "/1.0/packages/{name}.{origin}",
		UserOK: true,
		GET:    getPackageInfo,
		POST:   postPackage,
	}

	packageConfigCmd = &Command{
		Path: "/1.0/packages/{name}.{origin}/config",
		GET:  packageConfig,
		PUT:  packageConfig,
	}

	packageSvcsCmd = &Command{
		Path:   "/1.0/packages/{name}.{origin}/services",
		UserOK: true,
		GET:    packageService,
		PUT:    packageService,
	}

	packageSvcCmd = &Command{
		Path:   "/1.0/packages/{name}.{origin}/services/{service}",
		UserOK: true,
		GET:    packageService,
		PUT:    packageService,
	}

	packageSvcLogsCmd = &Command{
		Path: "/1.0/packages/{name}.{origin}/services/{service}/logs",
		GET:  getLogs,
	}

	operationCmd = &Command{
		Path:   "/1.0/operations/{uuid}",
		GET:    getOpInfo,
		DELETE: deleteOp,
	}

	capabilitiesCmd = &Command{
		Path:   "/1.0/capabilities",
		UserOK: true,
		GET:    getCapabilities,
		POST:   addCapability,
	}

	capabilityCmd = &Command{
		Path:   "/1.0/capabilities/{name}",
		DELETE: deleteCapability,
	}
)

func v1Get(c *Command, r *http.Request) Response {
	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError(err, "Unable to acquire lock")
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
	All() ([]snappy.Part, error)
	Updates() ([]snappy.Part, error)
}

var newRemoteRepo = func() metarepo {
	return snappy.NewMetaStoreRepository()
}

var newSystemRepo = func() metarepo {
	return snappy.NewSystemImageRepository()
}

var muxVars = mux.Vars

func getPackageInfo(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError(err, "Unable to acquire lock")
	}
	defer lock.Unlock()

	repo := newRemoteRepo()
	var part snappy.Part
	if parts, _ := repo.Details(name, origin); len(parts) > 0 {
		part = parts[0]
	}

	bag := lightweight.PartBagByName(name, origin)
	if bag == nil && part == nil {
		return NotFound
	}

	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError(nil, "router can't find route for package %s.%s", name, origin)
	}

	url, err := route.URL("name", name, "origin", origin)
	if err != nil {
		return InternalError(err, "route can't build URL for package %s.%s: %v", name, origin, err)
	}

	result := webify(bag.Map(part), url.String())

	return SyncResponse(result)
}

func webify(result map[string]string, resource string) map[string]string {
	result["resource"] = resource

	icon := result["icon"]
	if icon == "" || strings.HasPrefix(icon, "http") {
		return result
	}
	result["icon"] = ""

	route := appIconCmd.d.router.Get(appIconCmd.Path)
	if route != nil {
		url, err := route.URL("name", result["name"], "origin", result["origin"])
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
func getPackagesInfo(c *Command, r *http.Request) Response {
	route := c.d.router.Get(packageCmd.Path)
	if route == nil {
		return InternalError(nil, "router can't find route for packages")
	}

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError(err, "Unable to acquire lock")
	}
	defer lock.Unlock()

	sources := make([]string, 1, 3)
	sources[0] = "local"
	// we're not worried if the remote repos error out
	found, _ := newRemoteRepo().All()
	if len(found) > 0 {
		sources = append(sources, "store")
	}

	// systemRepo might be nil on all-snap systems
	if systemRepo := newSystemRepo(); systemRepo != nil {
		upd, _ := systemRepo.Updates()
		if len(upd) > 0 {
			sources = append(sources, "system-image")
		}
		found = append(found, upd...)
	}

	sort.Sort(byQN(found))

	bags := lightweight.AllPartBags()

	results := make(map[string]map[string]string)
	for _, part := range found {
		name := part.Name()
		origin := part.Origin()

		url, err := route.URL("name", name, "origin", origin)
		if err != nil {
			return InternalError(err, "can't get route to details for %s.%s: %v", name, origin, err)
		}

		fullname := name + "." + origin
		qn := snappy.QualifiedName(part)
		results[fullname] = webify(bags[qn].Map(part), url.String())
		delete(bags, qn)
	}

	for _, v := range bags {
		m := v.Map(nil)
		name := m["name"]
		origin := m["origin"]

		resource := "no resource URL for this resource"
		url, _ := route.URL("name", name, "origin", origin)
		if url != nil {
			resource = url.String()
		}

		results[name+"."+origin] = webify(m, resource)
	}

	return SyncResponse(map[string]interface{}{
		"packages": results,
		"sources":  sources,
		"paging": map[string]interface{}{
			"pages": 1,
			"page":  1,
			"count": len(results),
		},
	})
}

var findServices = snappy.FindServices

type svcDesc struct {
	Op     string                       `json:"op"`
	Spec   *snappy.ServiceYaml          `json:"spec"`
	Status *snappy.PackageServiceStatus `json:"status"`
}

func packageService(c *Command, r *http.Request) Response {
	route := c.d.router.Get(operationCmd.Path)
	if route == nil {
		return InternalError(nil, "router can't find route for operation")
	}

	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]
	if name == "" || origin == "" {
		return BadRequest(nil, "missing name or origin")
	}
	svcName := vars["service"]
	pkgName := name + "." + origin

	action := "status"

	if r.Method != "GET" {
		decoder := json.NewDecoder(r.Body)
		var cmd map[string]string
		if err := decoder.Decode(&cmd); err != nil {
			return BadRequest(err, "can't decode request body into service command: %v", err)
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
			return InternalError(err, "Unable to acquire lock")
		}

		defer func() {
			if !reachedAsync {
				lock.Unlock()
			}
		}()
	default:
		return BadRequest(nil, "unknown action %s", action)
	}

	bag := lightweight.PartBagByName(name, origin)
	idx := bag.ActiveIndex()
	if idx < 0 {
		return NotFound
	}

	ipart, err := bag.Load(idx)
	if err != nil {
		return InternalError(err, "unable to get load active package: %v", err)
	}

	part, ok := ipart.(*snappy.SnapPart)
	if !ok {
		return InternalError(nil, "active package is not a *snappy.SnapPart: %T", ipart)
	}
	svcs := part.ServiceYamls()

	if len(svcs) == 0 {
		return NotFound(nil, "package %q has no services", pkgName)
	}

	svcmap := make(map[string]*svcDesc, len(svcs))
	for i := range svcs {
		svcmap[svcs[i].Name] = &svcDesc{Spec: &svcs[i], Op: action}
	}

	if svcName != "" && svcmap[svcName] == nil {
		return NotFound(nil, "package %q has no service %q", pkgName, svcName)
	}

	// note findServices takes the *bare* name
	actor, err := findServices(name, svcName, &progress.NullProgress{})
	if err != nil {
		return InternalError(err, "no services for %q [%q] found: %v", pkgName, svcName, err)
	}

	f := func() interface{} {
		status, err := actor.ServiceStatus()
		if err != nil {
			logger.Noticef("unable to get status for %q [%q]: %v", pkgName, svcName, err)
			return err
		}

		for i := range status {
			if desc, ok := svcmap[status[i].ServiceName]; ok {
				desc.Status = status[i]
			} else {
				// shouldn't really happen, but can't hurt
				svcmap[status[i].ServiceName] = &svcDesc{Status: status[i]}
			}
		}

		if svcName == "" {
			return svcmap
		}

		return svcmap[svcName]
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
			logger.Noticef("unable to %s %q [%q]: %v\n", action, pkgName, svcName, err)
			return err
		}

		return f()
	}).Map(route))
}

func packageConfig(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]
	if name == "" || origin == "" {
		return BadRequest(nil, "missing name or origin")
	}
	pkgName := name + "." + origin

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError(err, "Unable to acquire lock")
	}
	defer lock.Unlock()

	bag := lightweight.PartBagByName(name, origin)
	if bag == nil {
		return NotFound
	}

	idx := bag.ActiveIndex()
	if idx < 0 {
		return BadRequest
	}

	part, err := bag.Load(idx)
	if err != nil {
		return InternalError(err, "unable to get load active package: %v", err)
	}

	bs, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return BadRequest(err, "reading config request body gave %v", err)
	}

	config, err := part.Config(bs)
	if err != nil {
		return InternalError(err, "unable to retrieve config for %s: %v", pkgName, err)
	}

	return SyncResponse(config)
}

type configSubtask struct {
	Status string      `json:"status"`
	Output interface{} `json:"output"`
}

func configMulti(c *Command, r *http.Request) Response {
	route := c.d.router.Get(operationCmd.Path)
	if route == nil {
		return InternalError(nil, "router can't find route for operation")
	}

	decoder := json.NewDecoder(r.Body)
	var pkgmap map[string]string
	if err := decoder.Decode(&pkgmap); err != nil {
		return BadRequest(err, "can't decode request body into map[string]string: %v", err)
	}

	return AsyncResponse(c.d.AddTask(func() interface{} {
		lock, err := lockfile.Lock(dirs.SnapLockFile, true)
		if err != nil {
			return err
		}
		defer lock.Unlock()

		rspmap := make(map[string]*configSubtask, len(pkgmap))
		bags := lightweight.AllPartBags()
		for pkg, cfg := range pkgmap {
			out := errorResult{}
			sub := configSubtask{Status: TaskFailed, Output: &out}
			rspmap[pkg] = &sub
			bag, ok := bags[pkg]
			if !ok {
				out.Str = snappy.ErrPackageNotFound.Error()
				out.Obj = snappy.ErrPackageNotFound
				continue
			}

			part, _ := bag.Load(bag.ActiveIndex())
			if part == nil {
				out.Str = snappy.ErrSnapNotActive.Error()
				out.Obj = snappy.ErrSnapNotActive
				continue
			}

			config, err := part.Config([]byte(cfg))
			if err != nil {
				out.Msg = "Config failed"
				out.Str = err.Error()
				out.Obj = err
				continue
			}
			sub.Status = TaskSucceeded
			sub.Output = config
		}

		return rspmap
	}).Map(route))
}

func getOpInfo(c *Command, r *http.Request) Response {
	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError(nil, "router can't find route for operation")
	}

	id := muxVars(r)["uuid"]
	task := c.d.GetTask(id)
	if task == nil {
		return NotFound
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
		return NotFound
	case errTaskStillRunning:
		return BadRequest
	default:
		return InternalError(err, "")
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

type packageInstruction struct {
	progress.NullProgress
	Action   string       `json:"action"`
	LeaveOld bool         `json:"leave_old"`
	License  *licenseData `json:"license"`
	pkg      string
}

// Agreed is part of the progress.Meter interface (q.v.)
// ask the user whether they agree to the given license's text
func (inst *packageInstruction) Agreed(intro, license string) bool {
	if inst.License == nil || !inst.License.Agreed || inst.License.Intro != intro || inst.License.License != license {
		inst.License = &licenseData{Intro: intro, License: license, Agreed: false}
		return false
	}

	return true
}

var snappyInstall = snappy.Install

func (inst *packageInstruction) install() interface{} {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}
	_, err := snappyInstall(inst.pkg, flags, inst)
	if err != nil {
		if inst.License != nil && snappy.IsLicenseNotAccepted(err) {
			return error(inst.License)
		}
		return err
	}

	// TODO: return the log
	// also to do: commands update their output dynamically, as it changes.
	return nil
}

func (inst *packageInstruction) update() interface{} {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}

	_, err := snappy.Update(inst.pkg, flags, inst)
	return err
}

func (inst *packageInstruction) remove() interface{} {
	flags := snappy.DoRemoveGC
	if inst.LeaveOld {
		flags = 0
	}

	return snappy.Remove(inst.pkg, flags, inst)
}

func (inst *packageInstruction) purge() interface{} {
	return snappy.Purge(inst.pkg, 0, inst)
}

func (inst *packageInstruction) rollback() interface{} {
	_, err := snappy.Rollback(inst.pkg, "", inst)
	return err
}

func (inst *packageInstruction) activate() interface{} {
	return snappy.SetActive(inst.pkg, true, inst)
}

func (inst *packageInstruction) deactivate() interface{} {
	return snappy.SetActive(inst.pkg, false, inst)
}

func (inst *packageInstruction) dispatch() func() interface{} {
	switch inst.Action {
	case "install":
		// TODO: licenses
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

func pkgActionDispatchImpl(inst *packageInstruction) func() interface{} {
	return inst.dispatch()
}

var pkgActionDispatch = pkgActionDispatchImpl

func postPackage(c *Command, r *http.Request) Response {
	route := c.d.router.Get(operationCmd.Path)
	if route == nil {
		return InternalError(nil, "router can't find route for operation")
	}

	decoder := json.NewDecoder(r.Body)
	var inst packageInstruction
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest(err, "can't decode request body into package instruction: %v", err)
	}

	vars := muxVars(r)
	inst.pkg = vars["name"] + "." + vars["origin"]

	f := pkgActionDispatch(&inst)
	if f == nil {
		return BadRequest(nil, "unknown action %s", inst.Action)
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
	return snappy.NewSnapPartFromSnapFile(filename, origin, unsignedOk)
}

var newSnap = newSnapImpl

func sideloadPackage(c *Command, r *http.Request) Response {
	route := c.d.router.Get(operationCmd.Path)
	if route == nil {
		return InternalError(nil, "router can't find route for operation")
	}

	body := r.Body
	unsignedOk := false
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/") {
		// spec says POSTs to sideload packages should be “a multipart file upload”

		_, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			return BadRequest(err, "")
		}

		form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(maxReadBuflen)
		if err != nil {
			return BadRequest(err, "")
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
					return BadRequest(err, "")
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
		return InternalError(err, "can't create tempfile: %v", err)
	}

	if _, err := io.Copy(tmpf, body); err != nil {
		os.Remove(tmpf.Name())
		return InternalError(err, "can't copy request into tempfile: %v", err)
	}

	return AsyncResponse(c.d.AddTask(func() interface{} {
		defer os.Remove(tmpf.Name())

		part, err := newSnap(tmpf.Name(), snappy.SideloadedOrigin, unsignedOk)
		if err != nil {
			return err
		}

		lock, err := lockfile.Lock(dirs.SnapLockFile, true)
		if err != nil {
			return err
		}
		defer lock.Unlock()

		name, err := part.Install(&progress.NullProgress{}, 0)
		if err != nil {
			return err
		}

		return name
	}).Map(route))
}

func getLogs(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	svcName := vars["service"]

	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError(err, "Unable to acquire lock")
	}
	defer lock.Unlock()

	actor, err := findServices(name, svcName, &progress.NullProgress{})
	if err != nil {
		return NotFound(err, "no services found for %q: %v", name, err)
	}

	rawlogs, err := actor.Logs()
	if err != nil {
		return InternalError(err, "unable to get logs for %q: %v", name, err)
	}

	logs := make([]map[string]interface{}, len(rawlogs))

	for i := range rawlogs {
		logs[i] = map[string]interface{}{
			"timestamp": rawlogs[i].RawTimestamp(),
			"message":   rawlogs[i].Message(),
			"raw":       rawlogs[i],
		}
	}

	return SyncResponse(logs)
}

func iconGet(name, origin string) Response {
	lock, err := lockfile.Lock(dirs.SnapLockFile, true)
	if err != nil {
		return InternalError(err, "Unable to acquire lock")
	}
	defer lock.Unlock()

	bag := lightweight.PartBagByName(name, origin)
	if bag == nil || len(bag.Versions) == 0 {
		return NotFound
	}

	part := bag.LoadBest()
	if part == nil {
		return NotFound
	}

	path := filepath.Clean(part.Icon())
	if !strings.HasPrefix(path, dirs.SnapAppsDir) && !strings.HasPrefix(path, dirs.SnapGadgetDir) {
		return BadRequest
	}

	return FileResponse(path)
}

func appIconGet(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]

	return iconGet(name, origin)
}

func getCapabilities(c *Command, r *http.Request) Response {
	return SyncResponse(map[string]interface{}{
		"capabilities": c.d.capRepo.Caps(),
	})
}

func addCapability(c *Command, r *http.Request) Response {
	decoder := json.NewDecoder(r.Body)
	var newCap caps.Capability
	if err := decoder.Decode(&newCap); err != nil || newCap.Type == nil {
		return BadRequest(err, "can't decode request body into a capability")
	}
	// Re-construct the perfect type object knowing just the type name that is
	// passed through the JSON representation.
	newType := c.d.capRepo.Type(newCap.Type.Name)
	if newType == nil {
		err := fmt.Errorf("unknown type name %q", newCap.Type.Name)
		return BadRequest(err, "can't add capability")
	}
	newCap.Type = newType
	if err := c.d.capRepo.Add(&newCap); err != nil {
		return BadRequest(err, "can't add capability")
	}
	return &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusCreated,
		Result: map[string]string{
			"resource": fmt.Sprintf("/1.0/capabilities/%s", newCap.Name),
		},
	}
}

func deleteCapability(c *Command, r *http.Request) Response {
	name := muxVars(r)["name"]
	err := c.d.capRepo.Remove(name)
	switch err.(type) {
	case nil:
		return SyncResponse(nil)
	case *caps.NotFoundError:
		return NotFound(err, "can't remove capability")
	default:
		return InternalError(err, "")
	}
}
