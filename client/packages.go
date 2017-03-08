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
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/snapcore/snapd/snap"
)

// Snap holds the data for a snap as obtained from snapd.
type Snap struct {
	ID              string        `json:"id"`
	Summary         string        `json:"summary"`
	Description     string        `json:"description"`
	DownloadSize    int64         `json:"download-size"`
	Icon            string        `json:"icon"`
	InstalledSize   int64         `json:"installed-size"`
	InstallDate     time.Time     `json:"install-date"`
	Name            string        `json:"name"`
	Developer       string        `json:"developer"`
	Status          string        `json:"status"`
	Type            string        `json:"type"`
	Version         string        `json:"version"`
	Channel         string        `json:"channel"`
	TrackingChannel string        `json:"tracking-channel"`
	Revision        snap.Revision `json:"revision"`
	Confinement     string        `json:"confinement"`
	Private         bool          `json:"private"`
	DevMode         bool          `json:"devmode"`
	JailMode        bool          `json:"jailmode"`
	TryMode         bool          `json:"trymode"`
	Apps            []AppInfo     `json:"apps"`
	Broken          string        `json:"broken"`
	Contact         string        `json:"contact"`

	Prices      map[string]float64 `json:"prices"`
	Screenshots []Screenshot       `json:"screenshots"`

	Channels map[string]*snap.ChannelSnapInfo `json:"channels"`
}

type AppInfo struct {
	Name    string   `json:"name"`
	Daemon  string   `json:"daemon"`
	Aliases []string `json:"aliases"`
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

// History returns the history of snap installs on the system
func (client *Client) History(name string) ([]*Snap, error) {
	var history []*Snap
	path := fmt.Sprintf("/v2/snaps/%s/history", name)
	_, err := client.doSync("GET", path, nil, nil, nil, &history)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve snap history for snap %q: %s", name, err)
	}
	return history, nil
}
