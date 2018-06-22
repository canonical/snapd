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
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/snapcore/snapd/snap"
)

// Snap holds the data for a snap as obtained from snapd.
type Snap struct {
	ID            string             `json:"id" help:"The unique snap-id"`
	Name          string             `json:"name" help:"The name of the snap"`
	Summary       string             `json:"summary" help:"The short summary"`
	Description   string             `json:"description" help:"The multi-line description"`
	Title         string             `json:"title,omitempty" help:"A human-readable name that may contain spaces"`
	DownloadSize  int64              `json:"download-size,omitempty" help:"The download size"`
	Icon          string             `json:"icon,omitempty"`
	InstalledSize int64              `json:"installed-size,omitempty" help:"The installed size in bytes"`
	InstallDate   time.Time          `json:"install-date,omitempty" help:"The date of installation (or empty)"`
	Publisher     *snap.StoreAccount `json:"publisher,omitempty"`
	// Developer is also the publisher's username for historic reasons.
	Developer        string        `json:"developer"`
	Status           string        `json:"status" help:"The active status"`
	Type             string        `json:"type" help:"The type (e.g. app)"`
	Base             string        `json:"base,omitempty" help:"The base snap used (if any)"`
	Version          string        `json:"version" help:"The human readable version"`
	Channel          string        `json:"channel" help:"The currently used channel"`
	TrackingChannel  string        `json:"tracking-channel,omitempty" help:"The currently tracked channel"`
	IgnoreValidation bool          `json:"ignore-validation"`
	Revision         snap.Revision `json:"revision" help:"The revision of the snap ("`
	Confinement      string        `json:"confinement" help:"The confinement used"`
	Private          bool          `json:"private" help:"True if this is a private snap"`
	DevMode          bool          `json:"devmode" help:"True if this snap is in devmode"`
	JailMode         bool          `json:"jailmode" help:"True if this snap is in jailmode"`
	TryMode          bool          `json:"trymode,omitempty" help:"True if this snap is in try-mode"`
	Apps             []AppInfo     `json:"apps,omitempty"`
	Broken           string        `json:"broken,omitempty" help:"True if this snap is currently broken"`
	Contact          string        `json:"contact"  help:"The contact for this snap"`
	License          string        `json:"license,omitempty" help:"The license as a SPDX expression"`
	CommonIDs        []string      `json:"common-ids,omitempty"`
	MountedFrom      string        `json:"mounted-from,omitempty"`

	Prices      map[string]float64 `json:"prices,omitempty" help:"The price of the snap"`
	Screenshots []Screenshot       `json:"screenshots,omitempty"`

	// The flattended channel map with $track/$risk
	Channels map[string]*snap.ChannelSnapInfo `json:"channels,omitempty"`

	// The ordered list of tracks that contains channels
	Tracks []string `json:"tracks,omitempty"`
}

func (s *Snap) MarshalJSON() ([]byte, error) {
	type auxSnap Snap // use auxiliary type so that Go does not call Snap.MarshalJSON()
	// separate type just for marshalling
	m := struct {
		auxSnap
		InstallDate *time.Time `json:"install-date,omitempty"`
	}{
		auxSnap: auxSnap(*s),
	}
	if !s.InstallDate.IsZero() {
		m.InstallDate = &s.InstallDate
	}
	return json.Marshal(&m)
}

type Screenshot struct {
	URL    string `json:"url"`
	Width  int64  `json:"width,omitempty"`
	Height int64  `json:"height,omitempty"`
}

// Statuses and types a snap may have.
const (
	StatusAvailable = "available"
	StatusInstalled = "installed"
	StatusActive    = "active"
	StatusRemoved   = "removed"
	StatusPriced    = "priced"

	TypeApp    = "app"
	TypeKernel = "kernel"
	TypeGadget = "gadget"
	TypeOS     = "os"

	StrictConfinement  = "strict"
	DevModeConfinement = "devmode"
	ClassicConfinement = "classic"
)

type ResultInfo struct {
	SuggestedCurrency string `json:"suggested-currency"`
}

// FindOptions supports exactly one of the following options:
// - Refresh: only return snaps that are refreshable
// - Private: return snaps that are private
// - Query: only return snaps that match the query string
type FindOptions struct {
	Refresh bool
	Private bool
	Prefix  bool
	Query   string
	Section string
}

var ErrNoSnapsInstalled = errors.New("no snaps installed")

type ListOptions struct {
	All bool
}

// List returns the list of all snaps installed on the system
// with names in the given list; if the list is empty, all snaps.
func (client *Client) List(names []string, opts *ListOptions) ([]*Snap, error) {
	if opts == nil {
		opts = &ListOptions{}
	}

	q := make(url.Values)
	if opts.All {
		q.Add("select", "all")
	}
	if len(names) > 0 {
		q.Add("snaps", strings.Join(names, ","))
	}

	snaps, _, err := client.snapsFromPath("/v2/snaps", q)
	if err != nil {
		return nil, err
	}

	if len(snaps) == 0 {
		return nil, ErrNoSnapsInstalled
	}

	return snaps, nil
}

// Sections returns the list of existing snap sections in the store
func (client *Client) Sections() ([]string, error) {
	var sections []string
	_, err := client.doSync("GET", "/v2/sections", nil, nil, nil, &sections)
	if err != nil {
		return nil, fmt.Errorf("cannot get snap sections: %s", err)
	}
	return sections, nil
}

// Find returns a list of snaps available for install from the
// store for this system and that match the query
func (client *Client) Find(opts *FindOptions) ([]*Snap, *ResultInfo, error) {
	if opts == nil {
		opts = &FindOptions{}
	}

	q := url.Values{}
	if opts.Prefix {
		q.Set("name", opts.Query+"*")
	} else {
		q.Set("q", opts.Query)
	}
	switch {
	case opts.Refresh && opts.Private:
		return nil, nil, fmt.Errorf("cannot specify refresh and private together")
	case opts.Refresh:
		q.Set("select", "refresh")
	case opts.Private:
		q.Set("select", "private")
	}
	if opts.Section != "" {
		q.Set("section", opts.Section)
	}

	return client.snapsFromPath("/v2/find", q)
}

func (client *Client) FindOne(name string) (*Snap, *ResultInfo, error) {
	q := url.Values{}
	q.Set("name", name)

	snaps, ri, err := client.snapsFromPath("/v2/find", q)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot find snap %q: %s", name, err)
	}

	if len(snaps) == 0 {
		return nil, nil, fmt.Errorf("cannot find snap %q", name)
	}

	return snaps[0], ri, nil
}

func (client *Client) snapsFromPath(path string, query url.Values) ([]*Snap, *ResultInfo, error) {
	var snaps []*Snap
	ri, err := client.doSync("GET", path, query, nil, nil, &snaps)
	if e, ok := err.(*Error); ok {
		return nil, nil, e
	}
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
