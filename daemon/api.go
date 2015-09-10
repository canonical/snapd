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
	"net/http"
	"sort"
	"strconv"

	"github.com/gorilla/mux"

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/release"
	"launchpad.net/snappy/snappy"
	"strings"
)

var api = []*Command{
	rootCmd,
	v1Cmd,
	packagesCmd,
	packageCmd,
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
	}
)

func v1Get(c *Command, r *http.Request) Response {
	rel := release.Get()
	return SyncResponse(map[string]string{
		"flavor":          rel.Flavor,
		"release":         rel.Series,
		"default_channel": rel.Channel,
		"api_compat":      "0",
	}).Self(c, r)
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
		if name != oldName && current != nil {
			// add to results
			url, err := route.URL("package", oldName)
			if err != nil {
				logger.Noticef("route can't build URL for package %s: %v", oldName, err)
				return InternalError
			}

			results[oldName] = parts2map(current, url.String())
			current = nil
		}
		oldName = name
		current = append(current, found[i])
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
