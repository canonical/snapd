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
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/servicestate"
)

var (
	shortStartHelp = i18n.G("Start services")
	longStartHelp  = i18n.G(`
The start command starts the given services of the snap. If executed from the
"configure" hook, the services will be started after the hook finishes.`)
)

func init() {
	addCommand("start", shortStartHelp, longStartHelp, func() command { return &startCommand{} })
}

type startCommand struct {
	baseCommand
	clientutil.ServiceScopeOptions
	Positional struct {
		ServiceNames []string `positional-arg-name:"<service>" required:"yes"`
	} `positional-args:"yes" required:"yes"`
	Enable bool `long:"enable" description:"Enable the specified services (see man systemctl for details)"`
}

func (c *startCommand) Execute(args []string) error {
	if err := c.Validate(); err != nil {
		return err
	}

	inst := servicestate.Instruction{
		Action: "start",
		Names:  c.Positional.ServiceNames,
		StartOptions: client.StartOptions{
			Enable: c.Enable,
		},
		Scope: c.Scope(),
		Users: c.Users(),
	}
	return runServiceCommand(c.context(), &inst)
}
