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
	"time"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
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
		if err := c.printPendingInfo(); err != nil {
			return err
		}
	}

	switch {
	case c.Proceed:
		return c.proceed()
	case c.Hold:
		return c.hold()
	}

	return nil
}

type updateDetails struct {
	Pending  string `yaml:"pending,omitempty"`
	Channel  string `yaml:"channel,omitempty"`
	Version  string `yaml:"version,omitempty"`
	Revision int    `yaml:"revision,omitempty"`
	// TODO: epoch
	Base    bool `yaml:"base"`
	Restart bool `yaml:"restart"`
}

// refreshCandidate is a subset of refreshCandidate defined by snapstate and
// stored in "refresh-candidates".
type refreshCandidate struct {
	Channel     string         `json:"channel,omitempty"`
	Version     string         `json:"version,omitempty"`
	SideInfo    *snap.SideInfo `json:"side-info,omitempty"`
	InstanceKey string         `json:"instance-key,omitempty"`
}

func getUpdateDetails(context *hookstate.Context) (*updateDetails, error) {
	context.Lock()
	defer context.Unlock()

	if context.IsEphemeral() {
		// TODO: support ephemeral context
		return nil, nil
	}

	var base, restart bool
	context.Get("base", &base)
	context.Get("restart", &restart)

	var candidates map[string]*refreshCandidate
	st := context.State()
	if err := st.Get("refresh-candidates", &candidates); err != nil {
		return nil, err
	}

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, context.InstanceName(), &snapst); err != nil {
		return nil, fmt.Errorf("internal error: cannot get snap state for %q: %v", context.InstanceName(), err)
	}

	var pending string
	switch {
	case snapst.RefreshInhibitedTime != nil:
		pending = "inhibited"
	case candidates[context.InstanceName()] != nil:
		pending = "ready"
	default:
		pending = "none"
	}

	up := updateDetails{
		Base:    base,
		Restart: restart,
		Pending: pending,
	}

	// try to find revision/version/channel info from refresh-candidates; it
	// may be missing if the hook is called for snap that is just affected by
	// refresh but not refreshed itself, in such case this data is not
	// displayed.
	if cand, ok := candidates[context.InstanceName()]; ok {
		up.Channel = cand.Channel
		up.Revision = cand.SideInfo.Revision.N
		up.Version = cand.Version
		return &up, nil
	}

	// refresh-hint not present, look up channel info in snapstate
	up.Channel = snapst.TrackingChannel
	return &up, nil
}

func (c *refreshCommand) printPendingInfo() error {
	details, err := getUpdateDetails(c.context())
	if err != nil {
		return err
	}
	// XXX: remove when ephemeral context is supported.
	if details == nil {
		return nil
	}
	out, err := yaml.Marshal(details)
	if err != nil {
		return err
	}
	c.printf("%s", string(out))
	return nil
}

func (c *refreshCommand) hold() error {
	ctx := c.context()
	ctx.Lock()
	defer ctx.Unlock()
	st := ctx.State()

	// cache the action so that hook handler can implement default behavior
	ctx.Cache("action", snapstate.GateAutoRefreshHold)

	var affecting []string
	if err := ctx.Get("affecting-snaps", &affecting); err != nil {
		return fmt.Errorf("internal error: cannot get affecting-snaps")
	}

	// no duration specified, use maximum allowed for this gating snap.
	var holdDuration time.Duration
	if err := snapstate.HoldRefresh(st, ctx.InstanceName(), holdDuration, affecting...); err != nil {
		// TODO: let a snap hold again once for 1h.
		return err
	}

	return nil
}

func (c *refreshCommand) proceed() error {
	ctx := c.context()
	ctx.Lock()
	defer ctx.Unlock()

	// cache the action, hook handler will trigger proceed logic; we cannot
	// call snapstate.ProceedWithRefresh() immediately as this would reset
	// holdState, allowing the snap to --hold with fresh duration limit.
	ctx.Cache("action", snapstate.GateAutoRefreshProceed)

	return nil
}
