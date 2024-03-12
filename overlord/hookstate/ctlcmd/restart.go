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
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/servicestate"
)

var (
	shortRestartHelp = i18n.G("Restart services")
	longRestartHelp  = i18n.G(`
The restart command restarts the given services of the snap. If executed from the
"configure" hook, the services will be restarted after the hook finishes.`)
)

func init() {
	addCommand("restart", shortRestartHelp, longRestartHelp, func() command { return &restartCommand{} })
}

type restartCommand struct {
	baseCommand
	userAndScopeOptions
	Positional struct {
		ServiceNames []string `positional-arg-name:"<service>" required:"yes"`
	} `positional-args:"yes" required:"yes"`
	Reload bool `long:"reload" description:"Reload the given services if they support it (see man systemctl for details)"`
}

func (c *restartCommand) Execute(args []string) error {
	if err := c.validateScopes(); err != nil {
		return err
	}

	inst := servicestate.Instruction{
		Action: "restart",
		Names:  c.Positional.ServiceNames,
		RestartOptions: client.RestartOptions{
			Reload: c.Reload,
		},
		Scope: c.serviceScope(),
		Users: c.serviceUsers(),
	}
	return runServiceCommand(c.context(), &inst)
}
