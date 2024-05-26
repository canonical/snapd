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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/devicestate"
)

var (
	shortRebootHelp = i18n.G("Control the reboot behavior of the system")
	longRebootHelp  = i18n.G(`
The reboot command can used from allowed hooks to control the reboot behavior of the system.

Currently it can only be invoked from gadget install-device during UC20 install mode. After invoking it from install-device with --halt or --poweroff the device will not reboot into run mode after finishing install mode but will instead either halt or power off. From install-device the effect is therefore not immediate but delayed until the end of installation itself.
`)
)

func init() {
	addCommand("reboot", shortRebootHelp, longRebootHelp, func() command { return &rebootCommand{} })
}

type rebootCommand struct {
	baseCommand

	Halt     bool `long:"halt"`
	Poweroff bool `long:"poweroff"`
}

func (c *rebootCommand) Execute([]string) error {
	ctx := mylog.Check2(c.ensureContext())

	if ctx.HookName() != "install-device" {
		return fmt.Errorf("cannot use reboot command outside of gadget install-device hook")
	}
	task, ok := ctx.Task()
	if !ok {
		return fmt.Errorf("internal error: inside gadget install-device hook but no task")
	}
	if !c.Halt && !c.Poweroff {
		return fmt.Errorf("either --halt or --poweroff must be specified")
	}
	if c.Halt && c.Poweroff {
		return fmt.Errorf("cannot specify both --halt and --poweroff")
	}

	ctx.Lock()
	defer ctx.Unlock()
	st := ctx.State()

	var restartTaskID string
	mylog.Check(task.Get("restart-task", &restartTaskID))

	restartTask := st.Task(restartTaskID)
	if restartTask == nil {
		// same error as TaskSnapSetup
		return fmt.Errorf("internal error: tasks are being pruned")
	}

	var op string
	if c.Halt {
		op = devicestate.RebootHaltOp
	} else if c.Poweroff {
		op = devicestate.RebootPoweroffOp
	}

	restartTask.Set("reboot", devicestate.RebootOptions{
		Op: op,
	})

	return nil
}
