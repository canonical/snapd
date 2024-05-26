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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/kernel/fde"
)

type fdeSetupRequestCommand struct {
	baseCommand
}

var shortFdeSetupRequestHelp = i18n.G("Obtain full disk encryption setup request")

var longFdeSetupRequestHelp = i18n.G(`
The fde-setup-request command is used inside the fde-setup hook. It will
return information about what operation for full-disk encryption is
requested and auxiliary data to complete this operation.

The fde-setup hook should do what is requested and then call
"snapctl fde-setup-result" and pass the result data to stdin.

Here is an example for how the fde-setup hook is called initially:
$ snapctl fde-setup-request
{"op":"features"}
$ echo  '{"features": []}' | snapctl fde-setup-result

Alternatively the hook could reply with:
$ echo '{"error":"hardware-unsupported"}' | snapctl fde-setup-result

And then it is called again with a request to do the initial key setup:
$ snapctl fde-setup-request
{"op":"initial-setup", "key": "key-to-seal"}
$ echo "{\"sealed-key\":\"$base64_encoded_sealed_key\"}" | snapctl fde-setup-result
`)

func init() {
	addCommand("fde-setup-request", shortFdeSetupRequestHelp, longFdeSetupRequestHelp, func() command { return &fdeSetupRequestCommand{} })
}

func (c *fdeSetupRequestCommand) Execute(args []string) error {
	context := mylog.Check2(c.ensureContext())

	context.Lock()
	defer context.Unlock()

	if context.HookName() != "fde-setup" {
		return fmt.Errorf("cannot use fde-setup-request outside of the fde-setup hook")
	}

	var fdeSetup fde.SetupRequest
	mylog.Check(context.Get("fde-setup-request", &fdeSetup))

	// Op is either "initial-setup" or "features"
	switch fdeSetup.Op {
	case "features", "initial-setup":
		// fine
	default:
		return fmt.Errorf("unknown fde-setup-request op %q", fdeSetup.Op)

	}

	bytes := mylog.Check2(json.Marshal(fdeSetup))

	c.printf("%s\n", string(bytes))

	return nil
}

type fdeSetupResultCommand struct {
	baseCommand
}

var (
	shortFdeSetupResultHelp = i18n.G("Set result for full disk encryption")
	longFdeSetupResultHelp  = i18n.G(`
The fde-setup-result command sets the result data for a fde-setup hook
reading it from stdin.

For example:
When the fde-setup hook is called with "op":"features:
$ echo '{"features": []}' | snapctl fde-setup-result

When the fde-setup hook is called with "op":"initial-setup":
$ echo "{\"sealed-key\":\"$base64_encoded_sealed_key\"}" | snapctl fde-setup-result
`)
)

func init() {
	addCommand("fde-setup-result", shortFdeSetupResultHelp, longFdeSetupResultHelp, func() command { return &fdeSetupResultCommand{} })
}

func (c *fdeSetupResultCommand) Execute(args []string) error {
	context := mylog.Check2(c.ensureContext())

	context.Lock()
	defer context.Unlock()

	if context.HookName() != "fde-setup" {
		return fmt.Errorf("cannot use fde-setup-result outside of the fde-setup hook")
	}

	var fdeSetupResult []byte
	mylog.Check(context.Get("stdin", &fdeSetupResult))

	if fdeSetupResult == nil {
		return fmt.Errorf("no result data found from stdin")
	}
	context.Set("fde-setup-result", fdeSetupResult)

	return nil
}
