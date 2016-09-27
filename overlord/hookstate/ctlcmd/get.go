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
	"github.com/snapcore/snapd/overlord/configstate"
)

type getCommand struct {
	baseCommand

	Positional struct {
		Keys []string `positional-arg-name:"<keys>" description:"keys of interest within the configuration" required:"1"`
	} `positional-args:"yes" required:"yes"`

	Document bool `short:"d" description:"always return document, even with single key"`
}

var shortGetHelp = i18n.G("Get snap configuration")
var longGetHelp = i18n.G(`
The get command retrieves the configuration parameters requested, for example:

    $ snapctl get foo
    bar

Or:

    $ snapctl get foo baz
    {
        "baz": "qux",
        "foo": "bar"
    }`)

func init() {
	addCommand("get", shortGetHelp, longGetHelp, func() command { return &getCommand{} })
}

func (c *getCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot get without a context")
	}

	patch := make(map[string]interface{})
	context.Lock()
	transaction := configstate.ContextTransaction(context)
	context.Unlock()

	for _, key := range c.Positional.Keys {
		var value interface{}
		if err := transaction.Get(c.context().SnapName(), key, &value); err != nil {
			return err
		}

		patch[key] = value
	}

	var confToPrint interface{} = patch
	if !c.Document && len(c.Positional.Keys) == 1 {
		confToPrint = patch[c.Positional.Keys[0]]
	}

	var bytes []byte
	if confToPrint != nil {
		var err error
		bytes, err = json.MarshalIndent(confToPrint, "", "\t")
		if err != nil {
			return err
		}
	}

	c.printf("%s", string(bytes))

	return nil
}
