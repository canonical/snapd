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

package client

import (
	"encoding/json"
	"fmt"
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
func (client *Client) Packages() (map[string]Package, error) {
	const errPrefix = "cannot list packages"

	var rsp response
	if err := client.do("GET", "/1.0/packages", nil, &rsp); err != nil {
		return nil, fmt.Errorf("%s: failed to communicate with server: %s", errPrefix, err)
	}
	if err := rsp.err(); err != nil {
		return nil, err
	}
	if rsp.Type != "sync" {
		return nil, fmt.Errorf("%s: expected sync response, got %q", errPrefix, rsp.Type)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(rsp.Result, &result); err != nil {
		return nil, fmt.Errorf("%s: failed to unmarshal response: %v", errPrefix, err)
	}

	packagesJSON := result["packages"]
	if packagesJSON == nil {
		return nil, fmt.Errorf("%s: response has no packages", errPrefix)
	}

	var packages map[string]Package
	if err := json.Unmarshal(packagesJSON, &packages); err != nil {
		return nil, fmt.Errorf("%s: failed to unmarshal packages: %v", errPrefix, err)
	}

	return packages, nil
}
