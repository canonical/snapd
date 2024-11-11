// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/i18n"
)

var (
	shortRemoveHelp = i18n.G("Remove components")
	longRemoveHelp  = i18n.G(`
The remove command removes components.
`)
)

func init() {
	addCommand("remove", shortRemoveHelp, longRemoveHelp, func() command { return &removeCommand{} })
}

type removeCommand struct {
	baseCommand
	Positional struct {
		Names []string `positional-arg-name:"<snap|snap+comp|+comp>" required:"yes" description:"Components to be removed (snap must be the caller snap if specified)."`
	} `positional-args:"yes"`
}

func (c *removeCommand) Execute([]string) error {
	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	comps, err := validateSnapAndCompsNames(c.Positional.Names, ctx.InstanceName())
	if err != nil {
		return err
	}

	if err := runSnapManagementCommand(ctx, managementCommand{
		operation: removeManagementCommand, components: comps}); err != nil {
		return err
	}

	return nil
}
