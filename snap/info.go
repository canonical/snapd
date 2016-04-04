// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snap

import (
	"time"
)

// Info provides information about snaps.
type Info struct {
	Name        string
	Developer   string
	Version     string
	Revision    int
	Type        Type
	Channel     string
	Description string
	Summary     string
	Apps        map[string]*AppInfo
	Plugs       map[string]*PlugInfo
	Slots       map[string]*SlotInfo
	// info for store hosted snaps
	Store *StoreInfo
}

// PlugInfo provides information about a plug.
type PlugInfo struct {
	Snap *Info

	Name      string
	Interface string
	Attrs     map[string]interface{}
	Label     string
	Apps      map[string]*AppInfo
}

// SlotInfo provides information about a slot.
type SlotInfo struct {
	Snap *Info

	Name      string
	Interface string
	Attrs     map[string]interface{}
	Label     string
	Apps      map[string]*AppInfo
}

// AppInfo provides information about a app.
type AppInfo struct {
	Snap *Info

	Name    string
	Command string
	Plugs   map[string]*PlugInfo
	Slots   map[string]*SlotInfo
}

// StoreInfo provides specific information for a store hosted snap.
type StoreInfo struct {
	LastUpdated     time.Time
	DownloadSha512  string
	DownloadSize    int64
	AnonDownloadURL string
	DownloadURL     string
	IconURL         string
}
