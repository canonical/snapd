// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/snap"
)

type refreshCommand struct {
	baseCommand

	Pending bool `long:"pending" description:"Show pending refreshes of the calling snap"`
	// these two options are mutually exclusive
	Proceed bool `long:"proceed" description:"Proceed with potentially disruptive refreshes"`
	Hold    bool `long:"hold" description:"Do not proceed with potentially disruptive refreshes"`
}

var shortRefreshHelp = i18n.G("The refresh command prints pending refreshes and can hold back disruptive ones.")
var longRefreshHelp = i18n.G(`
The refresh command prints pending refreshes of the calling snap and can hold
back disruptive refreshes of other snaps, such as refreshes of the kernel or
base snaps that can trigger a restart. This command can be used from the
gate-auto-refresh hook which is only run during auto-refresh.

Snap can query pending refreshes with:
    $ snapctl refresh --pending
    pending: ready
    channel: stable
    version: 2
    revision: 2
    base: false
    restart: false

The 'pending' flag can be "ready", "none" or "inhibited". It is set to "none"
when a snap has no pending refreshes. It is set to "ready" when there are
pending refreshes and to ”inhibited” when pending refreshes are being
held back because more or more snap applications are running with the
“refresh app awareness” feature enabled.

The "base" and "restart" flags indicate whether the base snap is going to be
updated and/or if a restart will occur, both of which are disruptive. A base
snap update can temporarily disrupt the starting of applications or hooks from
the snap.

To tell snapd to proceed with pending refreshes:
    $ snapctl refresh --pending --proceed

Note, a snap using --proceed cannot assume that the updates will occur as they
might be held back by other snaps.

To hold refresh for up to 90 days for the calling snap:
    $ snapctl refresh --pending --hold
`)

func init() {
	cmd := addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() command {
		return &refreshCommand{}
	})
	cmd.hidden = true
}

func (c *refreshCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot run without a context")
	}
	if context.IsEphemeral() {
		// TODO: handle this
		return fmt.Errorf("cannot run outside of gate-auto-refresh hook")
	}

	if context.HookName() != "gate-auto-refresh" {
		return fmt.Errorf("can only be used from gate-auto-refresh hook")
	}

	if c.Proceed && c.Hold {
		return fmt.Errorf("cannot use --proceed and --hold together")
	}

	if c.Pending {
		c.printPendingInfo()
	}

	if c.Proceed {
		return fmt.Errorf("not implemented yet")
	}
	if c.Hold {
		return fmt.Errorf("not implemented yet")
	}

	return nil
}

type updateDetails struct {
	pending  string
	channel  string
	version  string
	revision snap.Revision
	// TODO: epoch
	base    bool
	restart bool
}

func getUpdateDetails(context *hookstate.Context) *updateDetails {
	context.Lock()
	defer context.Unlock()

	if context.IsEphemeral() {
		// TODO: support ephemeral context
		return nil
	}

	var base, restart bool
	context.Get("base", &base)
	context.Get("restart", &restart)

	// TODO: get revision, version etc. from refresh-candidates.

	up := updateDetails{
		base:    base,
		restart: restart,
	}
	return &up
}

func (c *refreshCommand) printPendingInfo() {
	details := getUpdateDetails(c.context())
	// XXX: remove when ephemeral context is supported.
	if details == nil {
		return
	}
	c.printf("pending: %s\n", details.pending)
	c.printf("channel: %s\n", details.channel)
	if details.version != "" {
		c.printf("version: %s\n", details.version)
	}
	if !details.revision.Unset() {
		c.printf("revision: %s\n", details.revision)
	}
	c.printf("base: %v\n", details.base)
	c.printf("restart: %v\n", details.restart)
}
