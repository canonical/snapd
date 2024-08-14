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
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate"
)

type unsetCommand struct {
	baseCommand
	View bool `long:"view" description:"unset registry values in the view declared in the plug"`

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

	if !s.View {
		// unsetting options
		for _, confKey := range s.Positional.ConfKeys {
			tr.Set(context.InstanceName(), confKey, nil)
		}
		return nil
	}

	// unsetting registry data
	if !strings.HasPrefix(s.Positional.ConfKeys[0], ":") {
		return fmt.Errorf(i18n.G("cannot unset registry: plug must conform to format \":<plug-name>\": %s"), s.Positional.ConfKeys[0])
	}

	plugName := strings.TrimPrefix(s.Positional.ConfKeys[0], ":")
	if plugName == "" {
		return errors.New(i18n.G("cannot unset registry: plug name was not provided"))
	}

	if len(s.Positional.ConfKeys) == 1 {
		return errors.New(i18n.G("cannot unset registry: no paths provided to unset"))
	}

	confs := make(map[string]interface{}, len(s.Positional.ConfKeys)-1)
	for _, key := range s.Positional.ConfKeys[1:] {
		confs[key] = nil
	}

	return setRegistryValues(context, plugName, confs)
}
