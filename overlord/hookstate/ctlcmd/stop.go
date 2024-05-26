// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/servicestate"
)

type stopCommand struct {
	baseCommand
	clientutil.ServiceScopeOptions
	Positional struct {
		ServiceNames []string `positional-arg-name:"<service>" required:"yes"`
	} `positional-args:"yes" required:"yes"`
	Disable bool `long:"disable" description:"Disable the specified services (see man systemctl for details)"`
}

var (
	shortStopHelp = i18n.G("Stop services")
	longStopHelp  = i18n.G(`
The stop command stops the given services of the snap. If executed from the
"configure" hook, the services will be stopped after the hook finishes.`)
)

func init() {
	addCommand("stop", shortStopHelp, longStopHelp, func() command { return &stopCommand{} })
}

func (c *stopCommand) Execute(args []string) error {
	mylog.Check(c.Validate())

	inst := servicestate.Instruction{
		Action: "stop",
		Names:  c.Positional.ServiceNames,
		StopOptions: client.StopOptions{
			Disable: c.Disable,
		},
		Scope: c.Scope(),
		Users: c.Users(),
	}
	return runServiceCommand(c.context(), &inst)
}
