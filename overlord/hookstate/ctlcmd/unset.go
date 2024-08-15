// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"errors"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate"
)

type unsetCommand struct {
	baseCommand

	Positional struct {
		ConfKeys []string
	} `positional-args:"yes"`
}

var shortUnsetHelp = i18n.G("Remove configuration options")
var longUnsetHelp = i18n.G(`
The unset command removes the provided configuration options as requested.

$ snapctl unset name address

All configuration changes are persisted at once, and only after the
snap's configuration hook returns successfully.

Nested values may be removed via a dotted path:

$ snapctl unset user.name
`)

func init() {
	addCommand("unset", shortUnsetHelp, longUnsetHelp, func() command { return &unsetCommand{} })
}

func (s *unsetCommand) Execute(args []string) error {
	if len(s.Positional.ConfKeys) == 0 {
		return errors.New(i18n.G("unset which option?"))
	}

	context, err := s.ensureContext()
	if err != nil {
		return err
	}

	context.Lock()
	tr := configstate.ContextTransaction(context)
	context.Unlock()

	for _, confKey := range s.Positional.ConfKeys {
		tr.Set(context.InstanceName(), confKey, nil)
	}

	return nil
}
