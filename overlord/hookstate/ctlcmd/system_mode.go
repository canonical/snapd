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
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/strutil"
	"gopkg.in/yaml.v2"
)

type systemModeCommand struct {
	baseCommand
}

var shortSystemModeHelp = i18n.G("Get the current system mode information")

var longSystemModeHelp = i18n.G(` The system-mode command returns
 information about system mode the device is in, including the mode
 itself, whether the model snaps have been seeded, and whether the
 device is in factory mode. Its output is in YAML format.  The system
 mode will be one of run, recover, or install.
 Example output:

 $ snapctl system-mode
 system-mode: install
 seeded: true
 factory: true
`)

func init() {
	addCommand("system-mode", shortSystemModeHelp, longSystemModeHelp, func() command { return &systemModeCommand{} })
}

var devicestateSystemModeInfoFromState = devicestate.SystemModeInfoFromState

type systemModeResult struct {
	SystemMode string `yaml:"system-mode,omitempty"`
	Seeded     bool   `yaml:"seeded"`
	Factory    bool   `yaml:"factory,omitempty"`
}

func (c *systemModeCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot run system-mode without a context")
	}

	st := context.State()
	st.Lock()
	defer st.Unlock()

	smi, err := devicestateSystemModeInfoFromState(st)
	if err != nil {
		return err
	}

	res := systemModeResult{
		SystemMode: smi.Mode,
		Seeded:     smi.Seeded,
	}
	if strutil.ListContains(smi.BootFlags, "factory") {
		res.Factory = true
	}

	b, err := yaml.Marshal(res)
	if err != nil {
		return err
	}

	c.printf("%s", string(b))

	return nil
}
