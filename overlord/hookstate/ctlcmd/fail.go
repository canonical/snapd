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
	"github.com/snapcore/snapd/overlord/confdbstate"
)

type failCommand struct {
	baseCommand

	Positional struct {
		Reason string `positional-args:"true" positional-arg-name:":<rejection-reason>"`
	} `positional-args:"yes" required:"yes"`
}

var shortFailHelp = i18n.G("Fail a confdb change")
var longFailHelp = i18n.G(`
The fail command rejects the confdb changes currently being proposed,
providing a reason for the rejection. It may only be used in a
change-view-<plug> hook.
`)

func init() {
	info := addCommand("fail", shortFailHelp, longFailHelp, func() command {
		return &failCommand{}
	})
	info.hidden = true
}

func (c *failCommand) Execute(args []string) error {
	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	if err := validateConfdbFeatureFlag(ctx.State()); err != nil {
		return err
	}

	ctx.Lock()
	defer ctx.Unlock()

	if ctx.IsEphemeral() || !strings.HasPrefix(ctx.HookName(), "change-view-") {
		return errors.New(i18n.G(`cannot use "snapctl fail" outside of a "change-view" hook`))
	}

	t, _ := ctx.Task()
	tx, _, saveChanges, err := confdbstate.GetStoredTransaction(t)
	if err != nil {
		return fmt.Errorf(i18n.G("internal error: cannot get confdb transaction to fail: %v"), err)
	}

	tx.Abort(ctx.InstanceName(), c.Positional.Reason)
	saveChanges()
	return nil
}
