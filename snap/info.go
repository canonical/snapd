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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
)

// PlaceInfo offers all the information about where a snap and its data are located and exposed in the filesystem.
type PlaceInfo interface {
	// Name returns the name of the snap.
	Name() string

	// MountDir returns the base directory of the snap.
	MountDir() string

	// MountFile returns the path where the snap file that is mounted is installed.
	MountFile() string

	// DataDir returns the data directory of the snap.
	DataDir() string

	// CommonDataDir returns the data directory common across revisions of the snap.
	CommonDataDir() string

	// DataHomeDir returns the per user data directory of the snap.
	DataHomeDir() string

	// CommonDataHomeDir returns the per user data directory common across revisions of the snap.
	CommonDataHomeDir() string
}

// MinimalPlaceInfo returns a PlaceInfo with just the location information for a snap of the given name and revision.
func MinimalPlaceInfo(name string, revision Revision) PlaceInfo {
	return &Info{SideInfo: SideInfo{OfficialName: name, Revision: revision}}
}

// MountDir returns the base directory where it gets mounted of the snap with the given name and revision.
func MountDir(name string, revision Revision) string {
	return filepath.Join(dirs.SnapSnapsDir, name, revision.String())
}

// SideInfo holds snap metadata that is crucial for the tracking of
// snaps and for the working of the system offline and which is not
// included in snap.yaml or for which the store is the canonical
// source overriding snap.yaml content.
//
// It can be marshalled and will be stored in the system state for
// each currently installed snap revision so it needs to be evolved
// carefully.
//
// Information that can be taken directly from snap.yaml or that comes
// from the store but is not required for working offline should not
// end up in SideInfo.
type SideInfo struct {
	OfficialName      string   `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID            string   `yaml:"snap-id" json:"snap-id"`
	Revision          Revision `yaml:"revision" json:"revision"`
	Channel           string   `yaml:"channel,omitempty" json:"channel,omitempty"`
	Developer         string   `yaml:"developer,omitempty" json:"developer,omitempty"`
	EditedSummary     string   `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string   `yaml:"description,omitempty" json:"description,omitempty"`
	Size              int64    `yaml:"size,omitempty" json:"size,omitempty"`
	Sha512            string   `yaml:"sha512,omitempty" json:"sha512,omitempty"`
	Private           bool     `yaml:"private,omitempty" json:"private,omitempty"`
}

// Info provides information about snaps.
type Info struct {
	SuggestedName string
	Version       string
	Type          Type
	Architectures []string
	Assumes       []string

	OriginalSummary     string
	OriginalDescription string

	Environment map[string]string

	LicenseAgreement string
	LicenseVersion   string
	Epoch            string
	Confinement      ConfinementType
	Apps             map[string]*AppInfo
	Hooks            map[string]*HookInfo
	Plugs            map[string]*PlugInfo
	Slots            map[string]*SlotInfo

	// The information in all the remaining fields is not sourced from the snap blob itself.
	SideInfo

	// The information in these fields is ephemeral, available only from the store.
	AnonDownloadURL string
	DownloadURL     string

	IconURL string
	Prices  map[string]float64 `yaml:"prices,omitempty" json:"prices,omitempty"`
	MustBuy bool
}

// Name returns the blessed name for the snap.
func (s *Info) Name() string {
	if s.OfficialName != "" {
		return s.OfficialName
	}
	return s.SuggestedName
}

// Summary returns the blessed summary for the snap.
func (s *Info) Summary() string {
	if s.EditedSummary != "" {
		return s.EditedSummary
	}
	return s.OriginalSummary
}

// Description returns the blessed description for the snap.
func (s *Info) Description() string {
	if s.EditedDescription != "" {
		return s.EditedDescription
	}
	return s.OriginalDescription
}

// MountDir returns the base directory of the snap where it gets mounted.
func (s *Info) MountDir() string {
	return MountDir(s.Name(), s.Revision)
}

// MountFile returns the path where the snap file that is mounted is installed.
func (s *Info) MountFile() string {
	return filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%s.snap", s.Name(), s.Revision))
}

// DataDir returns the data directory of the snap.
func (s *Info) DataDir() string {
	return filepath.Join(dirs.SnapDataDir, s.Name(), s.Revision.String())
}

// CommonDataDir returns the data directory common across revisions of the snap.
func (s *Info) CommonDataDir() string {
	return filepath.Join(dirs.SnapDataDir, s.Name(), "common")
}

// DataHomeDir returns the per user data directory of the snap.
func (s *Info) DataHomeDir() string {
	return filepath.Join(dirs.SnapDataHomeGlob, s.Name(), s.Revision.String())
}

// CommonDataHomeDir returns the per user data directory common across revisions of the snap.
func (s *Info) CommonDataHomeDir() string {
	return filepath.Join(dirs.SnapDataHomeGlob, s.Name(), "common")
}

// sanity check that Info is a PlacInfo
var _ PlaceInfo = (*Info)(nil)

// PlugInfo provides information about a plug.
type PlugInfo struct {
	Snap *Info

	Name      string
	Interface string
	Attrs     map[string]interface{}
	Label     string
	Apps      map[string]*AppInfo
	Hooks     map[string]*HookInfo
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

	Daemon          string
	StopTimeout     timeout.Timeout
	StopCommand     string
	PostStopCommand string
	RestartCond     systemd.RestartCondition

	Socket       bool
	SocketMode   string
	ListenStream string

	// TODO: this should go away once we have more plumbing and can change
	// things vs refactor
	// https://github.com/snapcore/snapd/pull/794#discussion_r58688496
	BusName string

	Plugs map[string]*PlugInfo
	Slots map[string]*SlotInfo

	Environment map[string]string
}

// HookInfo provides information about a hook.
type HookInfo struct {
	Snap *Info

	Name  string
	Plugs map[string]*PlugInfo
}

// SecurityTag returns application-specific security tag.
//
// Security tags are used by various security subsystems as "profile names" and
// sometimes also as a part of the file name.
func (app *AppInfo) SecurityTag() string {
	return fmt.Sprintf("snap.%s.%s", app.Snap.Name(), app.Name)
}

// WrapperPath returns the path to wrapper invoking the app binary.
func (app *AppInfo) WrapperPath() string {
	var binName string
	if app.Name == app.Snap.Name() {
		binName = filepath.Base(app.Name)
	} else {
		binName = fmt.Sprintf("%s.%s", app.Snap.Name(), filepath.Base(app.Name))
	}

	return filepath.Join(dirs.SnapBinariesDir, binName)
}

func (app *AppInfo) launcherCommand(command string) string {
	securityTag := app.SecurityTag()
	return fmt.Sprintf("/usr/bin/ubuntu-core-launcher %s %s %s", securityTag, securityTag, filepath.Join(app.Snap.MountDir(), command))

}

// LauncherCommand returns the launcher command line to use when invoking the app binary.
func (app *AppInfo) LauncherCommand() string {
	return app.launcherCommand(app.Command)
}

// LauncherStopCommand returns the launcher command line to use when invoking the app stop command binary.
func (app *AppInfo) LauncherStopCommand() string {
	return app.launcherCommand(app.StopCommand)
}

// LauncherPostStopCommand returns the launcher command line to use when invoking the app post-stop command binary.
func (app *AppInfo) LauncherPostStopCommand() string {
	return app.launcherCommand(app.PostStopCommand)
}

// ServiceFile returns the systemd service file path for the daemon app.
func (app *AppInfo) ServiceFile() string {
	return filepath.Join(dirs.SnapServicesDir, app.SecurityTag()+".service")
}

// ServiceSocketFile returns the systemd socket file path for the daemon app.
func (app *AppInfo) ServiceSocketFile() string {
	return filepath.Join(dirs.SnapServicesDir, app.SecurityTag()+".socket")
}

func copyEnv(in map[string]string) map[string]string {
	out := make(map[string]string)
	for k, v := range in {
		out[k] = v
	}

	return out
}

// Env returns the app specific environment overrides
func (app *AppInfo) Env() []string {
	env := []string{}
	appEnv := copyEnv(app.Snap.Environment)
	for k, v := range app.Environment {
		appEnv[k] = v
	}
	for k, v := range appEnv {
		env = append(env, fmt.Sprintf("%s=%s\n", k, v))
	}
	return env
}

func infoFromSnapYamlWithSideInfo(meta []byte, si *SideInfo) (*Info, error) {
	info, err := InfoFromSnapYaml(meta)
	if err != nil {
		return nil, err
	}

	if si != nil {
		info.SideInfo = *si
	}

	return info, nil
}

// ReadInfo reads the snap information for the installed snap with the given name and given side-info.
func ReadInfo(name string, si *SideInfo) (*Info, error) {
	snapYamlFn := filepath.Join(MountDir(name, si.Revision), "meta", "snap.yaml")
	meta, err := ioutil.ReadFile(snapYamlFn)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot find mounted snap %q at revision %s", name, si.Revision)
	}
	if err != nil {
		return nil, err
	}

	return infoFromSnapYamlWithSideInfo(meta, si)
}

// ReadInfoFromSnapFile reads the snap information from the given File
// and completes it with the given side-info if this is not nil.
func ReadInfoFromSnapFile(snapf Container, si *SideInfo) (*Info, error) {
	meta, err := snapf.ReadFile("meta/snap.yaml")
	if err != nil {
		return nil, err
	}

	info, err := infoFromSnapYamlWithSideInfo(meta, si)
	if err != nil {
		return nil, err
	}

	err = Validate(info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// SplitSnapApp will split a string of the form `snap.app` into
// the `snap` and the `app` part. It also deals with the special
// case of snapName == appName.
func SplitSnapApp(snapApp string) (snap, app string) {
	l := strings.SplitN(snapApp, ".", 2)
	if len(l) < 2 {
		return l[0], l[0]
	}
	return l[0], l[1]
}
