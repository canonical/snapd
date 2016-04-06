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
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

// Info provides information about snaps.
type Info struct {
	Name          string
	Version       string
	Type          Type
	Architectures []string

	Description      string
	Summary          string
	LicenseAgreement string
	LicenseVersion   string
	Apps             map[string]*AppInfo
	Plugs            map[string]*PlugInfo
	Slots            map[string]*SlotInfo

	// The information in these fields is not present inside the snap blob itself.
	Revision        int
	Developer       string
	Channel         string
	Sha512          string
	Size            int64
	AnonDownloadURL string
	DownloadURL     string
	IconURL         string
}

// BaseDir returns the base directory of the snap.
func (s *Info) BaseDir() string {
	return filepath.Join(dirs.SnapSnapsDir, s.Name, s.Version)
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

	Daemon      string
	StopTimeout timeout.Timeout
	Stop        string
	PostStop    string
	RestartCond systemd.RestartCondition

	Socket       bool
	SocketMode   string
	ListenStream string

	// TODO: this should go away once we have more plumbing and can change
	// things vs refactor
	// https://github.com/ubuntu-core/snappy/pull/794#discussion_r58688496
	BusName string

	Plugs map[string]*PlugInfo
	Slots map[string]*SlotInfo
}
