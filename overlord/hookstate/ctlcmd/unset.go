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
	"strconv"
	"strings"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate"
)

type unsetCommand struct {
	baseCommand
	View bool `long:"view" description:"unset confdb values in the view declared in the plug"`

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

var longConfdbUnsetHelp = i18n.G(`
If the --view flag is used, 'snapctl unset' expects the name of a connected
interface plug referencing a confdb view. In that case, the command removes
the data at the provided paths according to the view referenced by the plug.
`)

func init() {
	if features.Confdb.IsEnabled() {
		longUnsetHelp += longConfdbUnsetHelp
	}

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

	if !s.View {
		context.Lock()
		tr := configstate.ContextTransaction(context)
		context.Unlock()

		// unsetting options
		for _, confKey := range s.Positional.ConfKeys {
			tr.Set(context.InstanceName(), confKey, nil)
		}
		return nil
	}

	if err := validateConfdbFeatureFlag(context.State()); err != nil {
		return err
	}

	// unsetting confdb data
	if !strings.HasPrefix(s.Positional.ConfKeys[0], ":") {
		return fmt.Errorf(i18n.G("cannot unset confdb: plug must conform to format \":<plug-name>\": %s"), s.Positional.ConfKeys[0])
	}

	plugName := strings.TrimPrefix(s.Positional.ConfKeys[0], ":")
	if plugName == "" {
		return errors.New(i18n.G("cannot unset confdb: plug name was not provided"))
	}

	if len(s.Positional.ConfKeys) == 1 {
		return errors.New(i18n.G("cannot unset confdb: no paths provided to unset"))
	}

	confs := make(map[string]any, len(s.Positional.ConfKeys)-1)
	for _, key := range s.Positional.ConfKeys[1:] {
		confs[key] = nil
	}

	uid, err := strconv.Atoi(s.baseCommand.uid)
	if err != nil {
		return err
	}

	return setConfdbValues(context, plugName, confs, uid)
}
