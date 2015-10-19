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

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/pkg/lightweight"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/release"
	"launchpad.net/snappy/snappy"
)

var api = []*Command{
	rootCmd,
	v1Cmd,
	metaIconCmd,
	appIconCmd,
	packagesCmd,
	packageCmd,
	packageConfigCmd,
	packageSvcCmd,
	packageSvcsCmd,
	packageSvcLogsCmd,
	operationCmd,
}

var (
	rootCmd = &Command{
		Path: "/",
		GET:  SyncResponse([]string{"/1.0"}).Self,
	}

	v1Cmd = &Command{
		Path: "/1.0",
		GET:  v1Get,
	}

	metaIconCmd = &Command{
		Path: "/1.0/icons/{icon}",
		GET:  metaIconGet,
	}

	appIconCmd = &Command{
		Path: "/1.0/icons/{name}.{origin}/icon",
		GET:  appIconGet,
	}

	packagesCmd = &Command{
		Path: "/1.0/packages",
		GET:  getPackagesInfo,
		POST: sideloadPackage,
		PUT:  configMulti,
	}

	packageCmd = &Command{
		Path: "/1.0/packages/{name}.{origin}",
		GET:  getPackageInfo,
		POST: postPackage,
	}

	packageConfigCmd = &Command{
		Path: "/1.0/packages/{name}.{origin}/config",
		GET:  packageConfig,
		PUT:  packageConfig,
	}

	packageSvcsCmd = &Command{
		Path: "/1.0/packages/{name}.{origin}/services",
		GET:  packageService,
		PUT:  packageService,
	}

	packageSvcCmd = &Command{
		Path: "/1.0/packages/{name}.{origin}/services/{service}",
		GET:  packageService,
		PUT:  packageService,
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
)

func v1Get(c *Command, r *http.Request) Response {
	rel := release.Get()
	m := map[string]string{
		"flavor":          rel.Flavor,
		"release":         rel.Series,
		"default_channel": rel.Channel,
		"api_compat":      "0",
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

	var route *mux.Route
	var args []string

	if strings.HasPrefix(icon, dirs.SnapIconsDir) {
		route = metaIconCmd.d.router.Get(metaIconCmd.Path)
		args = []string{"icon", icon[len(dirs.SnapIconsDir)+1:]}
	} else {
		route = appIconCmd.d.router.Get(appIconCmd.Path)
		args = []string{"name", result["name"], "origin", result["origin"]}
	}

	if route != nil {
		url, err := route.URL(args...)
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

	sources := make([]string, 1, 3)
	sources[0] = "local"
	// we're not worried if the remote repos error out
	found, _ := newRemoteRepo().All()
	if len(found) > 0 {
		sources = append(sources, "store")
	}

	upd, _ := newSystemRepo().Updates()
	if len(upd) > 0 {
		sources = append(sources, "system-image")
	}

	found = append(found, upd...)

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

	switch action {
	case "status", "start", "stop", "restart", "enable", "disable":
		// ok
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

	return AsyncResponse(c.d.AddTask(func() interface{} {
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

type packageInstruction struct {
	Action   string `json:"action"`
	LeaveOld bool   `json:"leave_old"`
	pkg      string
	prog     progress.Meter
}

func (inst *packageInstruction) install() interface{} {
	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}
	_, err := snappy.Install(inst.pkg, flags, inst.prog)

	if err != nil {
		return err
	}

	// TODO: return the log
	// also to do: commands update their output dynamically, as it changes.
	return nil
}

func (inst *packageInstruction) update() interface{} {
	// zomg :-(
	// TODO: query the store for just this package, instead of this

	flags := snappy.DoInstallGC
	if inst.LeaveOld {
		flags = 0
	}

	parts, err := snappy.ListUpdates()
	if err != nil {
		return err
	}

	for _, part := range parts {
		if snappy.QualifiedName(part) == inst.pkg {
			if _, err := part.Install(inst.prog, flags); err != nil {
				return err
			}
			return snappy.GarbageCollect(inst.pkg, flags, inst.prog)
		}
	}

	return "package is up to date"
}

func (inst *packageInstruction) remove() interface{} {
	flags := snappy.DoRemoveGC
	if inst.LeaveOld {
		flags = 0
	}

	return snappy.Remove(inst.pkg, flags, inst.prog)
}

func (inst *packageInstruction) purge() interface{} {
	return snappy.Purge(inst.pkg, 0, inst.prog)
}

func (inst *packageInstruction) rollback() interface{} {
	_, err := snappy.Rollback(inst.pkg, "", inst.prog)
	return err
}

func (inst *packageInstruction) activate() interface{} {
	return snappy.SetActive(inst.pkg, true, inst.prog)
}

func (inst *packageInstruction) deactivate() interface{} {
	return snappy.SetActive(inst.pkg, false, inst.prog)
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
	inst.prog = &progress.NullProgress{}

	f := pkgActionDispatch(&inst)
	if f == nil {
		return BadRequest(nil, "unknown action %s", inst.Action)
	}

	return AsyncResponse(c.d.AddTask(f).Map(route))
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

func metaIconGet(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["icon"]

	path := filepath.Join(dirs.SnapIconsDir, name)

	return FileResponse(path)
}

func appIconGet(c *Command, r *http.Request) Response {
	vars := muxVars(r)
	name := vars["name"]
	origin := vars["origin"]

	bag := lightweight.PartBagByName(name, origin)
	if bag == nil || len(bag.Versions) == 0 {
		return NotFound
	}

	part := bag.LoadBest()
	if part == nil {
		return NotFound
	}

	path := filepath.Clean(part.Icon())
	if !strings.HasPrefix(path, dirs.SnapAppsDir) && !strings.HasPrefix(path, dirs.SnapOemDir) {
		return BadRequest
	}

	return FileResponse(path)
}
