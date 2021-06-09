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

	"github.com/snapcore/snapd/osutil"
)

type ServiceState struct {
	ActiveState   string
	UnitFileState string
}

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
