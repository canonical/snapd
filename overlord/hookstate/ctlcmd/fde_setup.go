// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
)

type fdeSetupRequestCommand struct {
	baseCommand
}

var shortFdeSetupRequestHelp = i18n.G("Request setup of full disk encryption")
var longFdeSetupRequestHelp = i18n.G(`
The fde-setup-request command is used inside the fde-setup hook. It will
return information about what operation for full-disk encryption is
requested and auxiliary data to complete this operation.

The fde-setup hook should do what is requested and then call
"snapctl fde-setup-result" and pass the result data to stdin.

Here is an example for how the fde-setup hook is called initially:
$ snapctl fde-setup-request
{"op":"features"}
$ echo '[]' | snapctl fde-setup-result

Alternatively the hook could reply with:
$ echo '{"error":"hardware-unsupported"}' | snapctl fde-setup-result

And then it is called again with a request to do the initial key setup:
$ snapctl fde-setup-request
{"op":"initial-setup", "key": "key-to-seal", "key-name":"key-for-ubuntu-data"}
$ echo "$sealed_key" | snapctl fde-setup-result
`)

func init() {
	addCommand("fde-setup-request", shortFdeSetupRequestHelp, longFdeSetupRequestHelp, func() command { return &fdeSetupRequestCommand{} })
}

type fdeSetupJSON struct {
	// XXX: make "op" a type: "features", "initial-setup", "update" ?
	Op string `json:"op"`

	Key     []byte `json:"key,omitempty"`
	KeyName string `json:"key-name,omitempty"`

	// Model related fields, this will be set to follow the
	// secboot:SnapModel interface.
	//
	// XXX: do we need this to be a list? i.e. multiple models?
	Model map[string]string `json:"model,omitempty"`

	// TODO: provide LoadChains, KernelCmdline etc to support full
	//       tpm sealing
}

func (c *fdeSetupRequestCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot  without a context")
	}
	context.Lock()
	defer context.Unlock()

	var js fdeSetupJSON
	if err := context.Get("fde-op", &js.Op); err != nil {
		return fmt.Errorf("cannot get fde op from context: %v", err)
	}
	// Op is either "initial-setup" or "features"
	switch js.Op {
	case "features":
		// nothing
	case "initial-setup":
		if err := context.Get("fde-key", &js.Key); err != nil {
			return fmt.Errorf("cannot get fde key from context: %v", err)
		}
		if err := context.Get("fde-key-name", &js.KeyName); err != nil {
			return fmt.Errorf("cannot get fde key name from context: %v", err)
		}
		if err := context.Get("model", &js.Model); err != nil {
			return fmt.Errorf("cannot get model from context: %v", err)
		}
	default:
		return fmt.Errorf("unknown fde-setup-request op %q", js.Op)

	}

	bytes, err := json.Marshal(js)
	if err != nil {
		return fmt.Errorf("cannot json print fde key: %v", err)
	}
	c.printf("%s\n", string(bytes))

	return nil
}

type fdeSetupResultCommand struct {
	baseCommand
}

var shortFdeSetupResultHelp = i18n.G("Set result for full disk encryption")
var longFdeSetupResultHelp = i18n.G(`
The fde-setup-result command reads the result data for a fde-setup hook
from stdin.

E.g.
When the fde-setup hook is called with "op":"features:
$ echo "[]" | snapctl fde-setup-result

Or when the fde-setup hook is called with "op":"initial-setup":
$ echo "sealed-key" | snapctl fde-setup-result
`)

func init() {
	addCommand("fde-setup-result", shortFdeSetupResultHelp, longFdeSetupResultHelp, func() command { return &fdeSetupResultCommand{} })
}

func (c *fdeSetupResultCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot  without a context")
	}
	context.Lock()
	defer context.Unlock()

	var fdeSetupResult []byte
	if err := context.Get("stdin", &fdeSetupResult); err != nil {
		return fmt.Errorf("cannot get result from stdin: %v", err)
	}
	if fdeSetupResult == nil {
		return fmt.Errorf("no result data found on stdin")
	}
	task, ok := context.Task()
	if !ok {
		return fmt.Errorf("internal error: fdeSetupResultCommand called without task")
	}
	task.Set("fde-setup-result", fdeSetupResult)

	return nil
}
