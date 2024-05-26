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

package ctlcmd

import (
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/strutil"
)

type systemModeCommand struct {
	baseCommand
}

var shortSystemModeHelp = i18n.G("Get the current system mode and associated details")

var longSystemModeHelp = i18n.G(`
The system-mode command returns information about the device's current system mode.

This information includes the mode itself and whether the model snaps have been installed from the seed (seed-loaded). The system mode is either run, recover, or install.

Retrieved information can also include "factory mode" details: 'factory: true' declares whether the device booted an image flagged as for factory use. This flag can be set for convenience when building the image. No security sensitive decisions should be based on this bit alone.

The output is in YAML format. Example output:
    $ snapctl system-mode
    system-mode: install
    seed-loaded: true
    factory: true
`)

func init() {
	addCommand("system-mode", shortSystemModeHelp, longSystemModeHelp, func() command { return &systemModeCommand{} })
}

var devicestateSystemModeInfoFromState = devicestate.SystemModeInfoFromState

type systemModeResult struct {
	SystemMode string `yaml:"system-mode,omitempty"`
	Seeded     bool   `yaml:"seed-loaded"`
	Factory    bool   `yaml:"factory,omitempty"`
}

func (c *systemModeCommand) Execute(args []string) error {
	context := mylog.Check2(c.ensureContext())

	st := context.State()
	st.Lock()
	defer st.Unlock()

	smi := mylog.Check2(devicestateSystemModeInfoFromState(st))

	res := systemModeResult{
		SystemMode: smi.Mode,
		Seeded:     smi.Seeded,
	}
	if strutil.ListContains(smi.BootFlags, "factory") {
		res.Factory = true
	}

	b := mylog.Check2(yaml.Marshal(res))

	c.printf("%s", string(b))

	return nil
}
