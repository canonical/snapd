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
	"io"
	"time"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
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
		cmd[1] != "--property=Id,ActiveState,UnitFileState,Type" {
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
ActiveState=%s
UnitFileState=%s
Type=simple
`, unit, state.ActiveState, state.UnitFileState))...)
	}
	return output
}

type FakeSystemd struct {
	Mode     systemd.InstanceMode
	Reporter systemd.Reporter

	/* The user of this fake object can replace the implementation of the
	* various methods */
	MockedRemoveMountUnitFile func(baseDir string) error
}

/* For each of these methods, I'll create a "Mocked<MethodName>" field in the
 * FakeSystemd struct, to allow the caller to override it. See the
 * RemoveMountUnitFile() method below.
 */
func (s *FakeSystemd) DaemonReload() error                                   { return nil }
func (s *FakeSystemd) DaemonReexec() error                                   { return nil }
func (s *FakeSystemd) Enable(service string) error                           { return nil }
func (s *FakeSystemd) Disable(service string) error                          { return nil }
func (s *FakeSystemd) Start(service ...string) error                         { return nil }
func (s *FakeSystemd) StartNoBlock(service ...string) error                  { return nil }
func (s *FakeSystemd) Stop(service string, timeout time.Duration) error      { return nil }
func (s *FakeSystemd) Kill(service, signal, who string) error                { return nil }
func (s *FakeSystemd) Restart(service string, timeout time.Duration) error   { return nil }
func (s *FakeSystemd) ReloadOrRestart(service string) error                  { return nil }
func (s *FakeSystemd) RestartAll(service string) error                       { return nil }
func (s *FakeSystemd) Status(units ...string) ([]*systemd.UnitStatus, error) { return nil, nil }
func (s *FakeSystemd) InactiveEnterTimestamp(unit string) (time.Time, error) { return time.Time{}, nil }
func (s *FakeSystemd) IsEnabled(service string) (bool, error)                { return false, nil }
func (s *FakeSystemd) IsActive(service string) (bool, error)                 { return false, nil }
func (s *FakeSystemd) LogReader(services []string, n int, follow bool) (io.ReadCloser, error) {
	return nil, nil
}
func (s *FakeSystemd) AddMountUnitFile(name, revision, what, where, fstype string) (string, error) {
	return "", nil
}

func (s *FakeSystemd) RemoveMountUnitFile(baseDir string) error {
	if s.MockedRemoveMountUnitFile != nil {
		return s.MockedRemoveMountUnitFile(baseDir)
	}
	return nil
}

func (s *FakeSystemd) Mask(service string) error                             { return nil }
func (s *FakeSystemd) Unmask(service string) error                           { return nil }
func (s *FakeSystemd) Mount(what, where string, options ...string) error     { return nil }
func (s *FakeSystemd) Umount(whatOrWhere string) error                       { return nil }
func (s *FakeSystemd) CurrentMemoryUsage(unit string) (quantity.Size, error) { return 0, nil }
func (s *FakeSystemd) CurrentTasksCount(unit string) (uint64, error)         { return 0, nil }
