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
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"io/ioutil"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/release"
	"launchpad.net/snappy/snappy"
)

var api = []*Command{
	rootCmd,
	v1Cmd,
	packagesCmd,
	packageCmd,
	packageConfigCmd,
	packageSvcCmd,
	packageSvcsCmd,
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

	packagesCmd = &Command{
		Path: "/1.0/packages",
		GET:  getPackagesInfo,
	}

	packageCmd = &Command{
		Path: "/1.0/packages/{package}",
		GET:  getPackageInfo,
		POST: postPackage,
	}

	packageConfigCmd = &Command{
		Path: "/1.0/packages/{package}/config",
		GET:  packageConfig,
		PUT:  packageConfig,
	}

	packageSvcsCmd = &Command{
		Path: "/1.0/packages/{package}/services",
		GET:  packageService,
		PUT:  packageService,
	}

	packageSvcCmd = &Command{
		Path: "/1.0/packages/{package}/services/{service}",
		GET:  packageService,
		PUT:  packageService,
	}

	operationCmd = &Command{
		Path: "/1.0/operations/{uuid}",
		GET:  getOpInfo,
	}
)

func v1Get(c *Command, r *http.Request) Response {
	rel := release.Get()
	return SyncResponse(map[string]string{
		"flavor":          rel.Flavor,
		"release":         rel.Series,
		"default_channel": rel.Channel,
		"api_compat":      "0",
	})
}

type metarepo interface {
	Details(string) ([]snappy.Part, error)
	All() ([]snappy.Part, error)
}

var newRepo = func() metarepo {
	return snappy.NewMetaRepository()
}

var newLocalRepo = func() metarepo {
	return snappy.NewMetaLocalRepository()
}

var newRemoteRepo = func() metarepo {
	return snappy.NewMetaStoreRepository()
}

var muxVars = mux.Vars

func getPackageInfo(c *Command, r *http.Request) Response {
	reqName := muxVars(r)["package"]
	if reqName == "" {
		// can't happen, i think? mux won't let it
		return BadRequest
	}
	repo := newRepo()
	found, err := repo.Details(reqName)
	if err != nil {
		if err == snappy.ErrPackageNotFound {
			return NotFound
		}

		return InternalError
	}

	if len(found) == 0 {
		return NotFound
	}

	name := snappy.QualifiedName(found[0])
	for i := range found {
		n := snappy.QualifiedName(found[i])
		if n != name {
			logger.Noticef("in getting details for %q: found parts with different qualified names: %q and %q.", reqName, name, n)
			return InternalError
		}
	}

	route := c.d.router.Get(c.Path)
	if route == nil {
		logger.Noticef("router can't find route for package %s", name)
		return InternalError
	}

	url, err := route.URL("package", name)
	if err != nil {
		logger.Noticef("route can't build URL for package %s: %v", name, err)
		return InternalError
	}

	result := parts2map(found, url.String())

	return SyncResponse(result)
}

// parts2map takes a slice of parts with the same name and returns a
// single map with that part's metadata (including rollback_available
// & etc).
func parts2map(parts []snappy.Part, resource string) map[string]string {
	if len(parts) == 0 {
		return nil
	}

	// TODO: handle multiple results in parts; set rollback_available; set update_available
	part := parts[0]
	var status string
	if part.IsInstalled() {
		if part.IsActive() {
			status = "active"
		} else {
			// can't really happen
			status = "installed"
		}
	} else {
		status = "not installed"
	}
	// TODO: check for removed and transients (extend the Part interface for removed; check ops for transients)

	icon := part.Icon()
	if strings.HasPrefix(icon, iconPath) {
		icon = iconPrefix + icon[len(iconPath):]
	}

	result := map[string]string{
		"icon":           icon,
		"name":           part.Name(),
		"origin":         part.Origin(),
		"resource":       resource,
		"status":         status,
		"type":           string(part.Type()),
		"vendor":         part.Vendor(),
		"version":        part.Version(),
		"description":    part.Description(),
		"installed_size": strconv.FormatInt(part.InstalledSize(), 10),
		"download_size":  strconv.FormatInt(part.DownloadSize(), 10),
	}

	return result
}

type byQN []snappy.Part

func (ps byQN) Len() int      { return len(ps) }
func (ps byQN) Swap(a, b int) { ps[a], ps[b] = ps[b], ps[a] }
func (ps byQN) Less(a, b int) bool {
	return snappy.QualifiedName(ps[a]) < snappy.QualifiedName(ps[b])
}

func addPart(results map[string]map[string]string, current []snappy.Part, oldName string, route *mux.Route) []snappy.Part {
	url, err := route.URL("package", oldName)
	if err != nil {
		logger.Noticef("route can't build URL for package %s: %v", oldName, err)
		return current
	}

	results[oldName] = parts2map(current, url.String())

	return nil
}

// plural!
func getPackagesInfo(c *Command, r *http.Request) Response {
	route := c.d.router.Get(packageCmd.Path)
	if route == nil {
		logger.Noticef("router can't find route for packages")
		return InternalError
	}

	sources := r.URL.Query().Get("sources")
	var repo metarepo
	switch sources {
	case "local":
		repo = newLocalRepo()
	case "remote":
		repo = newRemoteRepo()
	default:
		repo = newRepo()
	}

	found, err := repo.All()
	if err != nil {
		if err == snappy.ErrPackageNotFound {
			return NotFound
		}

		return InternalError
	}

	if len(found) == 0 {
		return NotFound
	}

	sort.Sort(byQN(found))

	results := make(map[string]map[string]string)
	var current []snappy.Part
	var oldName string
	for i := range found {
		name := snappy.QualifiedName(found[i])
		if name != oldName && len(current) > 0 {
			current = addPart(results, current, oldName, route)
			if current != nil {
				return InternalError
			}
		}
		oldName = name
		current = append(current, found[i])
	}
	if len(current) > 0 {
		current = addPart(results, current, oldName, route)
		if current != nil {
			return InternalError
		}
	}

	return SyncResponse(map[string]interface{}{
		"packages": results,
		"paging": map[string]interface{}{
			"pages": 1,
			"page":  1,
			"count": len(results),
		},
	})
}

func getActivePkg(qn string) ([]snappy.Part, error) {
	// TODO: below should be rolled into ActiveSnapByName
	// (specifically: ActiveSnapByname should know about origins)
	repo := newLocalRepo()
	all, err := repo.All()
	if err != nil {
		logger.Noticef("unable to get package list: %v", err)
		return nil, err
	}

	parts := all[:0]
	for _, part := range snappy.FindSnapsByName(qn, all) {
		if part.IsActive() {
			parts = append(parts, part)
		}
	}

	return parts, nil
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
		logger.Noticef("router can't find route for operation")
		return InternalError
	}

	vars := muxVars(r)
	pkgName := vars["package"]
	if pkgName == "" {
		return BadRequest
	}
	svcName := vars["service"]

	action := "status"

	if r.Method != "GET" {
		decoder := json.NewDecoder(r.Body)
		var cmd map[string]string
		if err := decoder.Decode(&cmd); err != nil {
			logger.Noticef("can't decode request body into service command: %v", err)
			return BadRequest
		}

		action = cmd["action"]
	}

	switch action {
	case "status", "start", "stop", "restart", "enable", "disable":
		// ok
	default:
		return BadRequest
	}

	parts, err := getActivePkg(pkgName)
	if err != nil {
		return InternalError
	}

	switch len(parts) {
	default:
		return BadRequest
	case 0:
		return NotFound
	case 1:
		// yay, or something
	}

	part, ok := parts[0].(snappy.ServiceYamler)
	if !ok {
		return InternalError
	}
	svcs := part.ServiceYamls()

	if len(svcs) == 0 {
		return NotFound
	}

	svcmap := make(map[string]*svcDesc, len(svcs))
	for i := range svcs {
		svcmap[svcs[i].Name] = &svcDesc{Spec: &svcs[i], Op: action}
	}

	if svcName != "" && svcmap[svcName] == nil {
		return NotFound
	}

	actor, err := findServices(pkgName, svcName, &progress.NullProgress{})
	if err != nil {
		logger.Noticef("no services found: %v", err)
		return NotFound
	}

	f := func() interface{} {
		status, err := actor.ServiceStatus()
		if err != nil {
			logger.Noticef("unable to get status for %s [%s]: %v", pkgName, svcName, err)
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
			logger.Noticef("unable to %s %s [%s]: %v\n", action, pkgName, svcName, err)
			return err
		}

		return f()
	}).Map(route))
}

func packageConfig(c *Command, r *http.Request) Response {
	pkgName := muxVars(r)["package"]
	if pkgName == "" {
		return BadRequest
	}

	parts, err := getActivePkg(pkgName)
	if err != nil {
		return InternalError
	}

	switch len(parts) {
	default:
		return BadRequest
	case 0:
		return NotFound
	case 1:
		// yay, or something
	}

	bs, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logger.Noticef("reading config request body gave %v", err)
		return BadRequest
	}

	config, err := parts[0].Config(bs)
	if err != nil {
		logger.Noticef("unable to retrieve config for %s: %v", pkgName, err)
		return InternalError
	}

	return SyncResponse(config)
}

func getOpInfo(c *Command, r *http.Request) Response {
	route := c.d.router.Get(c.Path)
	if route == nil {
		logger.Noticef("router can't find route for operation")
		return InternalError
	}

	id := muxVars(r)["uuid"]
	if id == "" {
		// can't happen, i think? mux won't let it
		return BadRequest
	}

	task := c.d.GetTask(id)
	if task == nil {
		return NotFound
	}

	return SyncResponse(task.Map(route))
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
		logger.Noticef("router can't find route for operation")
		return InternalError
	}

	decoder := json.NewDecoder(r.Body)
	var inst packageInstruction
	if err := decoder.Decode(&inst); err != nil {
		logger.Noticef("can't decode request body into package instruction: %v", err)
		return BadRequest
	}

	inst.pkg = muxVars(r)["package"]
	inst.prog = &progress.NullProgress{}

	f := pkgActionDispatch(&inst)
	if f == nil {
		logger.Noticef("unknown action %s", inst.Action)
		return BadRequest
	}

	return AsyncResponse(c.d.AddTask(f).Map(route))
}
