// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

// SideInfo holds snap metadata that is not included in snap.yaml or for which the store is the canonical source.
// It can be marshalled both as JSON and YAML.
type SideInfo struct {
	OfficialName string `yaml:"name,omitempty" json:"name,omitempty"`
	// XXX likely we want also snap-id
	Revision          int    `yaml:"revision" json:"revision"`
	Channel           string `yaml:"channel,omitempty" json:"channel,omitempty"`
	Developer         string `yaml:"developer,omitempty" json:"developer,omitempty"`
	EditedSummary     string `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string `yaml:"description,omitempty" json:"description,omitempty"`
	Size              int64  `yaml:"size,omitempty" json:"size,omitempty"`
	Sha512            string `yaml:"sha512,omitempty" json:"sha512,omitempty"`
	IconURL           string `yaml:"icon-url,omitempty" json:"icon-url,omitempty"`
}

// Info provides information about snaps.
type Info struct {
	SuggestedName string
	Version       string
	Type          Type
	Architectures []string

	OriginalSummary     string
	OriginalDescription string

	LicenseAgreement string
	LicenseVersion   string
	Apps             map[string]*AppInfo
	Plugs            map[string]*PlugInfo
	Slots            map[string]*SlotInfo

	// The information in these fields is not present inside the snap blob itself.
	SideInfo

	AnonDownloadURL string
	DownloadURL     string
}

// Name returns the blessed name for the snap.
func (s *Info) ZName() string {
	if s.OfficialName != "" {
		return s.OfficialName
	}
	return s.SuggestedName
}

// Summary returns the blessed summary for the snap.
func (s *Info) ZSummary() string {
	if s.EditedSummary != "" {
		return s.EditedSummary
	}
	return s.OriginalSummary
}

// Description returns the blessed description for the snap.
func (s *Info) ZDescription() string {
	if s.EditedDescription != "" {
		return s.EditedDescription
	}
	return s.OriginalDescription
}

// BaseDir returns the base directory of the snap.
func (s *Info) BaseDir() string {
	return filepath.Join(dirs.SnapSnapsDir, s.ZName(), s.Version)
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
