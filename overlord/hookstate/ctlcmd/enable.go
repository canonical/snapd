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
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/servicestate"
)

type enableCommand struct {
	baseCommand
	clientutil.ServiceScopeOptions
	Positional struct {
		ServiceNames []string `positional-arg-name:"<service>" required:"yes"`
	} `positional-args:"yes" required:"yes"`
}

var (
	shortEnableHelp = i18n.G("Enable services")
	longEnableHelp  = i18n.G(`
The enable command enables the given services of the snap. If executed from the
"configure" hook or "default-configure" hook, the services will be enabled after the hook finishes.`)
)

func init() {
	addCommand("enable", shortEnableHelp, longEnableHelp, func() command { return &enableCommand{} })
}

func (c *enableCommand) Execute(args []string) error {
	if err := c.Validate(); err != nil {
		return err
	}

	inst := servicestate.Instruction{
		Action: "enable",
		Names:  c.Positional.ServiceNames,
		Scope:  c.Scope(),
		Users:  c.Users(),
	}
	return runServiceCommand(c.context(), &inst)
}
