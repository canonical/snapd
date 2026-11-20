// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package systemdtest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

type ServiceState struct {
	ActiveState   string
	UnitFileState string
}

// HandleMockAllUnitsActiveOutput returns the output for systemctl in the case
// where units have the state as described by states.
// If `cmd` is the command issued by systemd.Status(), this function returns
// the output to be produced by the command so that the queried services will
// appear having the ActiveState and UnitFileState according to the data
// passed in the `states` map.
func HandleMockAllUnitsActiveOutput(cmd []string, states map[string]ServiceState) []byte {
	osutil.MustBeTestBinary("mocking systemctl output can only be done from tests")
	if cmd[0] != "show" ||
		!strutil.ListContains([]string{
			// extended properties for services and mounts
			"--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload",
			// base properties for everything else
			"--property=Id,ActiveState,UnitFileState,Names",
		}, cmd[1]) {
		return nil
	}
	var output []byte
	for _, unit := range cmd[2:] {
		if len(output) > 0 {
			output = append(output, byte('\n'))
		}
		state, ok := states[unit]
		if !ok {
			state = ServiceState{"active", "enabled"}
		}
		output = append(output, []byte(fmt.Sprintf(`Id=%s
Names=%s
ActiveState=%s
UnitFileState=%s
Type=simple
NeedDaemonReload=no
`, unit, unit, state.ActiveState, state.UnitFileState))...)
	}
	return output
}

type MountUnitInfo struct {
	Description  string
	Where        string
	FragmentPath string
}

// HandleMockListMountUnitsOutput returns the output for systemctl in the case
// where units have the state as described by states.
// If `cmd` is the command issued by systemd.Status(), this function returns
// the output to be produced by the command so that the queried services will
// appear having the ActiveState and UnitFileState according to the data
// passed in the `states` map.
func HandleMockListMountUnitsOutput(cmd []string, mounts []MountUnitInfo) ([]byte, bool) {
	osutil.MustBeTestBinary("mocking systemctl output can only be done from tests")
	if cmd[0] != "show" ||
		cmd[1] != "--property=Description,Where,FragmentPath" {
		return nil, false
	}
	var output []byte
	for _, mountInfo := range mounts {
		if len(output) > 0 {
			output = append(output, byte('\n'))
		}
		output = append(output, []byte(fmt.Sprintf(`Description=%s
Where=%s
FragmentPath=%s
`, mountInfo.Description, mountInfo.Where, mountInfo.FragmentPath))...)
	}
	return output, true
}

// OriginMountUnit describes a mount unit with a snapd origin, as created for
// example by "snapctl mount".
type OriginMountUnit struct {
	Description string
	Where       string
	Origin      string
}

// MockSystemctlForOriginMountUnits writes unit files for the given mount
// units into dir and mocks systemctl so that both loaded and installed mount
// unit listings report them. It is meant for exercising the real
// systemd.ListMountUnits implementation.
func MockSystemctlForOriginMountUnits(dir string, units []OriginMountUnit) (restore func(), err error) {
	osutil.MustBeTestBinary("mocking systemctl output can only be done from tests")
	var unitFiles []string
	var mounts []MountUnitInfo
	for _, u := range units {
		unitFile := systemd.EscapeUnitNamePath(u.Where) + ".mount"
		path := filepath.Join(dir, unitFile)
		contents := fmt.Sprintf("[Unit]\nDescription=%s\n\n[Mount]\nWhere=%s\n\n[Install]\nX-SnapdOrigin=%s\n",
			u.Description, u.Where, u.Origin)
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			return nil, err
		}
		unitFiles = append(unitFiles, unitFile)
		mounts = append(mounts, MountUnitInfo{Description: u.Description, Where: u.Where, FragmentPath: path})
	}
	restore = systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		if out, ok := HandleMockListMountUnitsOutput(args, mounts); ok {
			return out, nil
		}
		if len(args) > 0 && args[0] == "list-unit-files" {
			var out []byte
			for _, unitFile := range unitFiles {
				out = append(out, []byte(unitFile+" enabled enabled\n")...)
			}
			return out, nil
		}
		return nil, fmt.Errorf("unexpected systemctl call: %q", args)
	})
	return restore, nil
}

type FakeSystemd struct {
	systemd.Systemd

	ConfigureMountUnitOptionsCalls   []ParamsForConfigureMountUnitOptions
	ConfigureMountUnitOptionsResults ResultForConfigureMountUnitOptions

	EnsureMountUnitFileCalls  []*systemd.MountUnitOptions
	EnsureMountUnitFileResult ResultForEnsureMountUnitFile

	RemoveMountUnitFileCalls  []string
	RemoveMountUnitFileResult error

	ListMountUnitsCalls  []ParamsForListMountUnits
	ListMountUnitsResult ResultForListMountUnits
	// ListMountUnitsDelegate, if set, is used to compute the result of
	// ListMountUnits instead of ListMountUnitsResult.
	ListMountUnitsDelegate systemd.Systemd

	StopCalls  [][]string
	StopResult error

	StartCalls  [][]string
	StartResult error
}

type ParamsForConfigureMountUnitOptions struct {
	What               string
	Fstype             string
	StartBeforeDrivers bool
}

type ResultForConfigureMountUnitOptions struct {
	Fstype        string
	Options       []string
	MountUnitType systemd.MountUnitType
}

type ResultForEnsureMountUnitFile struct {
	Path string
	Err  error
}

type ParamsForListMountUnits struct {
	SnapName string
	Origin   string
	Filter   systemd.MountUnitFilter
}

type ResultForListMountUnits struct {
	MountPoints []string
	Err         error
}

func (s *FakeSystemd) ConfigureMountUnitOptions(o *systemd.MountUnitOptions, fstype string, startBeforeDrivers bool) error {
	s.ConfigureMountUnitOptionsCalls = append(s.ConfigureMountUnitOptionsCalls, ParamsForConfigureMountUnitOptions{
		What:               o.What,
		Fstype:             fstype,
		StartBeforeDrivers: startBeforeDrivers,
	})

	o.Fstype = s.ConfigureMountUnitOptionsResults.Fstype
	o.MountUnitType = s.ConfigureMountUnitOptionsResults.MountUnitType
	o.Options = s.ConfigureMountUnitOptionsResults.Options

	return nil
}

func (s *FakeSystemd) EnsureMountUnitFile(mountOptions *systemd.MountUnitOptions) (string, error) {
	s.EnsureMountUnitFileCalls = append(s.EnsureMountUnitFileCalls, mountOptions)
	return s.EnsureMountUnitFileResult.Path, s.EnsureMountUnitFileResult.Err
}

func (s *FakeSystemd) RemoveMountUnitFile(mountDir string) error {
	s.RemoveMountUnitFileCalls = append(s.RemoveMountUnitFileCalls, mountDir)
	return s.RemoveMountUnitFileResult
}

func (s *FakeSystemd) ListMountUnits(snapName, origin string, filter systemd.MountUnitFilter) ([]string, error) {
	s.ListMountUnitsCalls = append(s.ListMountUnitsCalls,
		ParamsForListMountUnits{SnapName: snapName, Origin: origin, Filter: filter})
	if s.ListMountUnitsDelegate != nil {
		return s.ListMountUnitsDelegate.ListMountUnits(snapName, origin, filter)
	}
	return s.ListMountUnitsResult.MountPoints, s.ListMountUnitsResult.Err
}

func (s *FakeSystemd) Stop(units []string) error {
	s.StopCalls = append(s.StopCalls, append([]string{}, units...))
	return s.StopResult
}

func (s *FakeSystemd) Start(units []string) error {
	s.StartCalls = append(s.StartCalls, append([]string{}, units...))
	return s.StartResult
}
