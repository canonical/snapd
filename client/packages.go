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
	"time"
)

// Snap holds the data for a snap as obtained from snapd.
type Snap struct {
	ID            string    `json:"id"`
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
	Revision      int       `json:"revision"`

	Prices map[string]float64 `json:"prices"`
}

// Statuses and types a snap may have.
const (
	StatusAvailable = "available"
	StatusInstalled = "installed"
	StatusActive    = "active"
	StatusRemoved   = "removed"

	TypeApp    = "app"
	TypeKernel = "kernel"
	TypeGadget = "gadget"
	TypeOS     = "os"
)

type ResultInfo struct {
	SuggestedCurrency string `json:"suggested-currency"`
}

// ListSnaps returns the list of all snaps installed on the system
// with names in the given list; if the list is empty, all snaps.
func (client *Client) ListSnaps(names []string) ([]*Snap, error) {
	snaps, _, err := client.snapsFromPath("/v2/snaps", nil)
	if err != nil {
		return nil, err
	}

	if len(names) == 0 {
		return snaps, nil
	}

	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}

	var result []*Snap
	for _, snap := range snaps {
		if wanted[snap.Name] {
			result = append(result, snap)
		}
	}

	return result, nil
}

// FindSnaps returns a list of snaps available for install from the
// store for this system and that match the query
func (client *Client) FindSnaps(query string) ([]*Snap, *ResultInfo, error) {
	q := url.Values{}

	q.Set("q", query)

	return client.snapsFromPath("/v2/find", q)
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
