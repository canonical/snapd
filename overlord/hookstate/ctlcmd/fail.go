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
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/registrystate"
)

type failCommand struct {
	baseCommand

	View bool `long:"view" description:"fail ongoing registry changes"`

	Positional struct {
		Reason string `positional-args:"true" positional-arg-name:":<rejection-reason>"`
	} `positional-args:"yes" required:"yes"`
}

var shortFailHelp = i18n.G("Fail a registry change")
var longFailHelp = i18n.G(`
The fail command rejects the registry changes currently being proposed,
providing a reason for the rejection. It may only be used with the --view flag
in a change-view hook.
`)

func init() {
	addCommand("fail", shortFailHelp, longFailHelp, func() command {
		return &failCommand{}
	})
}

func (c *failCommand) Execute(args []string) error {
	if !c.View {
		return errors.New(i18n.G("cannot use `snapctl fail` without --view flag"))
	}

	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	ctx.Lock()
	defer ctx.Unlock()

	if ctx.IsEphemeral() || !strings.HasPrefix(ctx.HookName(), "change-view-") {
		return errors.New(i18n.G("cannot use `snapctl fail` outside of a \"change-view\" hook"))
	}

	t, _ := ctx.Task()
	tx, commitTask, err := registrystate.GetStoredTransaction(t)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot get registry transaction to fail: %v"), err)
	}

	tx.Abort(ctx.InstanceName(), c.Positional.Reason)
	commitTask.Set("registry-transaction", tx)
	return nil
}
