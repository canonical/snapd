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
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Snap holds the data for a snap as obtained from snapd.
type Snap struct {
	Summary       string    `json:"summary"`
	Description   string    `json:"description"`
	DownloadSize  int64     `json:"download-size"`
	Icon          string    `json:"icon"`
	InstalledSize int64     `json:"installed-size"`
	InstallDate   time.Time `json:"install-date"`
	Name          string    `json:"name"`
	Developer     string    `json:"developer"`
	Status        string    `json:"status"`
	Type          string    `json:"type"`
	Version       string    `json:"version"`

	Prices map[string]float64 `json:"prices"`
}

// SnapFilter is used to filter snaps by source, name and/or type
type SnapFilter struct {
	Sources []string
	Types   []string
	Query   string
}

// Statuses and types a snap may have.
const (
	StatusAvailable = "available"
	StatusInstalled = "installed"
	StatusActive    = "active"
	StatusRemoved   = "removed"

	TypeApp       = "app"
	TypeFramework = "framework"
	TypeKernel    = "kernel"
	TypeGadget    = "gadget"
	TypeOS        = "os"
)

type ResultInfo struct {
	Sources           []string `json:"sources"`
	SuggestedCurrency string   `json:"suggested-currency"`
}

// Snaps returns the list of all snaps installed on the system and
// available for install from the store for this system.
func (client *Client) Snaps() ([]*Snap, *ResultInfo, error) {
	return client.snapsFromPath("/v2/snaps", nil)
}

// FilterSnaps returns a list of snaps per Snaps() but filtered by source, name
// and/or type
func (client *Client) FilterSnaps(filter SnapFilter) ([]*Snap, *ResultInfo, error) {
	q := url.Values{}

	if filter.Query != "" {
		q.Set("q", filter.Query)
	}

	if len(filter.Sources) > 0 {
		q.Set("sources", strings.Join(filter.Sources, ","))
	}

	if len(filter.Types) > 0 {
		q.Set("types", strings.Join(filter.Types, ","))
	}

	return client.snapsFromPath("/v2/snaps", q)
}

func (client *Client) snapsFromPath(path string, query url.Values) ([]*Snap, *ResultInfo, error) {
	var snaps []*Snap
	ri, err := client.doSync("GET", path, query, nil, nil, &snaps)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list snaps: %s", err)
	}
	return snaps, ri, nil
}

// Snap returns the most recently published revision of the snap with the
// provided name.
func (client *Client) Snap(name string) (*Snap, *ResultInfo, error) {
	var snap *Snap
	path := fmt.Sprintf("/v2/snaps/%s", name)
	ri, err := client.doSync("GET", path, nil, nil, nil, &snap)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot retrieve snap %q: %s", name, err)
	}
	return snap, ri, nil
}
