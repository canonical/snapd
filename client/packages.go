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
	"net/http"
	"net/url"
	"strings"
)

// Snap holds the data for a snap as obtained from snapd.
type Snap struct {
	Description   string `json:"description"`
	DownloadSize  int64  `json:"download_size"`
	Icon          string `json:"icon"`
	InstalledSize int64  `json:"installed_size"`
	Name          string `json:"name"`
	Origin        string `json:"origin"`
	Status        string `json:"status"`
	Type          string `json:"type"`
	Version       string `json:"version"`
}

// SnapFilter is used to filter snaps by source, name and/or type
type SnapFilter struct {
	Sources []string
	Types   []string
	Query   string
}

// Statuses and types a snap may have.
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

// Snaps returns the list of all snaps installed on the system and
// available for install from the store for this system.
func (client *Client) Snaps() (map[string]*Snap, error) {
	return client.snapsFromPath("/2.0/snaps", nil)
}

// FilterSnaps returns a list of snaps per Snaps() but filtered by source, name
// and/or type
func (client *Client) FilterSnaps(filter SnapFilter) (map[string]*Snap, error) {
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

	return client.snapsFromPath("/2.0/snaps", q)
}

func (client *Client) snapsFromPath(path string, query url.Values) (map[string]*Snap, error) {
	const errPrefix = "cannot list snaps"

	var result map[string]json.RawMessage
	if err := client.doSync("GET", path, query, nil, &result); err != nil {
		return nil, fmt.Errorf("%s: %s", errPrefix, err)
	}

	snapsJSON := result["snaps"]
	if snapsJSON == nil {
		return nil, fmt.Errorf("%s: response has no snaps", errPrefix)
	}

	var snaps map[string]*Snap
	if err := json.Unmarshal(snapsJSON, &snaps); err != nil {
		return nil, fmt.Errorf("%s: failed to unmarshal snaps: %v", errPrefix, err)
	}

	return snaps, nil
}

// Snap returns the most recently published revision of the snap with the
// provided name.
func (client *Client) Snap(name string) (*Snap, error) {
	var pkg *Snap

	path := fmt.Sprintf("/2.0/snaps/%s", name)
	if err := client.doSync("GET", path, nil, nil, &pkg); err != nil {
		return nil, fmt.Errorf("cannot retrieve snap %q: %s", name, err)
	}

	return pkg, nil
}

// RemoveSnap removes the snap with the given name, returning the UUID of the
// background operation upon success
func (client *Client) RemoveSnap(name string) (string, error) {
	const errPrefix = "cannot remove snap"
	const opPrefix = "/2.0/operations/"
	var rsp response

	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"remove"}`)

	if err := client.do("POST", path, nil, body, &rsp); err != nil {
		return "", fmt.Errorf("%s: %s", errPrefix, err)
	}
	if err := rsp.err(); err != nil {
		return "", err
	}
	if rsp.Type != "async" {
		return "", fmt.Errorf("%s: expected async response, got %q", errPrefix, rsp.Type)
	}
	if rsp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("%s: operation not accepted", errPrefix)
	}

	var operation map[string]interface{}
	if err := json.Unmarshal(rsp.Result, &operation); err != nil {
		return "", fmt.Errorf("%s: failed to unmarshal operation: %v", errPrefix, err)
	}

	resource, ok := operation["resource"].(string)
	if !ok {
		return "", fmt.Errorf("%s: operation has no resource", errPrefix)
	}
	if !strings.HasPrefix(resource, opPrefix) {
		return "", fmt.Errorf("%s: invalid resource", errPrefix)
	}

	return resource[len(opPrefix):], nil
}
