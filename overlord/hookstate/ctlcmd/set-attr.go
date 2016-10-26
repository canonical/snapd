// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type setAttrCommand struct {
	baseCommand

	Positional struct {
		AttrValues []string `positional-arg-name:"key=value" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var shortSetAttrHelp = i18n.G("Changes configuration options")
var longSetAttrHelp = i18n.G(`
The setattr command changes the provided interface attributes as requested.

    $ snapctl set-attr path=/dev/ttyS0 usb-product=1000

Attributes can only be set in the context of prepare-plug- and prepare-slot-
interface hooks.
`)

func init() {
	addCommand("set-attr", shortSetAttrHelp, longSetAttrHelp, func() command { return &setAttrCommand{} })
}

func (s *setAttrCommand) Execute(args []string) error {
	context := s.context()
	if context == nil {
		return fmt.Errorf("cannot set without a context")
	}

	context.Lock()
	defer context.Unlock()

	var attrs map[string]string
	if err := context.Get("attributes", &attrs); err != nil {
		if err == state.ErrNoState {
			attrs = make(map[string]string)
		} else {
			return err
		}
	}

	for _, attrValue := range s.Positional.AttrValues {
		parts := strings.SplitN(attrValue, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid parameter: %q (want key=value)"), attrValue)
		}
		attrs[parts[0]] = parts[1]
	}
	s.context().Set("attributes", attrs)

	return nil
}
