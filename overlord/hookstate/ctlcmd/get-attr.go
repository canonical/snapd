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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type getAttrCommand struct {
	baseCommand

	Positional struct {
		Attributes []string `positional-arg-name:"<keys>" description:"option keys" required:"yes"`
	} `positional-args:"yes" required:"yes"`
}

var shortGetAttrHelp = i18n.G("Prints values of interface attributes")
var longGetAttrHelp = i18n.G(`
The get command prints values of interface attributes for current connection.

    $ snapctl get-attr serialpath
    /dev/ttyS0

If multiple attribute names are provided, a document is returned:

    $ snapctl get serialpath usb-vendor
    {
        "serialpath": "/dev/ttyS0",
        "usb-vendor": "1000"
    }
`)

func init() {
	addCommand("get-attr", shortGetAttrHelp, longGetAttrHelp, func() command { return &getAttrCommand{} })
}

func (c *getAttrCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot get-attr without a context")
	}

	var err error
	var attributes map[string]interface{}
	context.Lock()
	err = context.Get("attributes", &attributes)
	context.Unlock()

	if err == state.ErrNoState {
		return fmt.Errorf(i18n.G("no attributes found"))
	}
	if err != nil {
		return err
	}

	attrsToPrint := make(map[string]interface{})
	for _, key := range c.Positional.Attributes {
		if value, ok := attributes[key]; ok {
			attrsToPrint[key] = value
		} else {
			return fmt.Errorf(i18n.G("unknown attribute %q"), key)
		}
	}

	var bytes []byte
	bytes, err = json.MarshalIndent(attrsToPrint, "", "\t")
	if err != nil {
		return err
	}

	c.printf("%s\n", string(bytes))

	return nil
}
