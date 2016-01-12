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

package client

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Package represents a Snap package
type Package struct {
	Description   string `json:"description"`
	DownloadSize  int64  `json:"download_size,string"`
	Icon          string `json:"icon"`
	InstalledSize int64  `json:"installed_size,string"`
	Name          string `json:"name"`
	Origin        string `json:"origin"`
	Status        string `json:"status"`
	Type          string `json:"type"`
	Version       string `json:"version"`
}

// Packages are collections of Package items
type Packages []Package

// Statuses and types a Package may have
const (
	StatusNotInstalled = "not installed"
	StatusInstalled    = "installed"
	StatusActive       = "active"
	StatusRemoved      = "removed"

	TypeApp       = "app"
	TypeFramework = "framework"
	TypeKernel    = "kernel"
	TypeGadget    = "gadget"
	TypeOS        = "os"
)

// Packages returns the list of packages the system can handle
func (client *Client) Packages() (Packages, error) {
	const errPrefix = "cannot list packages"

	var result map[string]json.RawMessage
	if err := client.doSync("GET", "/1.0/packages", nil, &result); err != nil {
		return nil, fmt.Errorf("%s: %s", errPrefix, err)
	}

	packagesJSON := result["packages"]
	if packagesJSON == nil {
		return nil, fmt.Errorf("%s: response has no packages", errPrefix)
	}

	var packageMap map[string]Package
	if err := json.Unmarshal(packagesJSON, &packageMap); err != nil {
		return nil, fmt.Errorf("%s: failed to unmarshal packages: %v", errPrefix, err)
	}

	var packages Packages
	for _, pkg := range packageMap {
		packages = append(packages, pkg)
	}

	return packages, nil
}

// IsInstalled returns true if the Package is currently installed
func (p Package) IsInstalled() bool {
	return p.Status == StatusInstalled || p.Status == StatusActive
}

// HasNameContaining returns true if the Package name contains the query. The
// comparison is case-insensitive
func (p Package) HasNameContaining(query string) bool {
	return strings.Contains(strings.ToLower(p.Name), strings.ToLower(query))
}

// HasTypeInSet returns true if the Package type is a member of the given list
// of types
func (p Package) HasTypeInSet(types []string) bool {
	for _, t := range types {
		if p.Type == t {
			return true
		}
	}

	return false
}

// Installed returns the installed items from Packages
func (p Packages) Installed() Packages {
	var packages Packages

	for _, pkg := range p {
		if pkg.IsInstalled() {
			packages = append(packages, pkg)
		}
	}

	return packages
}

// NamesContaining returns the items from Packages with names containing the
// query
func (p Packages) NamesContaining(query string) Packages {
	var packages Packages

	for _, pkg := range p {
		if pkg.HasNameContaining(query) {
			packages = append(packages, pkg)
		}
	}

	return packages
}

// TypesInSet returns items from Packages with types contained in the given list
func (p Packages) TypesInSet(types []string) Packages {
	var packages Packages

	for _, pkg := range p {
		if pkg.HasTypeInSet(types) {
			packages = append(packages, pkg)
		}
	}

	return packages
}

type byName Packages

func (n byName) Len() int           { return len(n) }
func (n byName) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n byName) Less(i, j int) bool { return n[i].Name < n[j].Name }

// SortByName returns items from Packages sorted by name
func (p Packages) SortByName() Packages {
	sorted := make(Packages, len(p))

	copy(sorted, p)
	sort.Sort(byName(sorted))

	return sorted
}
