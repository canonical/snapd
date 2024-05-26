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
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var autoRefreshForGatingSnap = snapstate.AutoRefreshForGatingSnap

type refreshCommand struct {
	baseCommand

	Pending bool `long:"pending" description:"Show pending refreshes of the calling snap"`
	// these two options are mutually exclusive
	Proceed bool `long:"proceed" description:"Proceed with potentially disruptive refreshes"`
	Hold    bool `long:"hold" description:"Do not proceed with potentially disruptive refreshes"`

	PrintInhibitLock bool `long:"show-lock" description:"Show the value of the run inhibit lock held during refreshes (empty means not held)"`
}

var (
	shortRefreshHelp = i18n.G("The refresh command prints pending refreshes and can hold back disruptive ones.")
	longRefreshHelp  = i18n.G(`
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
)

func init() {
	cmd := addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() command {
		return &refreshCommand{}
	})
	cmd.hidden = true
}

func (c *refreshCommand) Execute(args []string) error {
	context := mylog.Check2(c.ensureContext())

	if !context.IsEphemeral() && context.HookName() != "gate-auto-refresh" {
		return fmt.Errorf("can only be used from gate-auto-refresh hook")
	}

	var which string
	for _, opt := range []struct {
		val  bool
		name string
	}{
		{c.PrintInhibitLock, "--show-lock"},
		{c.Hold, "--hold"},
		{c.Proceed, "--proceed"},
	} {
		if opt.val && which != "" {
			return fmt.Errorf("cannot use %s and %s together", opt.name, which)
		}
		if opt.val {
			which = opt.name
		}
	}

	// --pending --proceed is a verbose way of saying --proceed, so only
	// print pending if proceed wasn't requested.
	if c.Pending && !c.Proceed {
		mylog.Check(c.printPendingInfo())
	}

	switch {
	case c.Proceed:
		return c.proceed()
	case c.Hold:
		return c.hold()
	case c.PrintInhibitLock:
		return c.printInhibitLockHint()
	}

	return nil
}

type updateDetails struct {
	Pending   string `yaml:"pending,omitempty"`
	Channel   string `yaml:"channel,omitempty"`
	CohortKey string `yaml:"cohort,omitempty"`
	Version   string `yaml:"version,omitempty"`
	Revision  int    `yaml:"revision,omitempty"`
	// TODO: epoch
	Base    bool `yaml:"base"`
	Restart bool `yaml:"restart"`
}

type holdDetails struct {
	Hold string `yaml:"hold"`
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

	st := context.State()

	affected := mylog.Check2(snapstate.AffectedByRefreshCandidates(st))

	var base, restart bool
	if affectedInfo, ok := affected[context.InstanceName()]; ok {
		base = affectedInfo.Base
		restart = affectedInfo.Restart
	}

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(st, context.InstanceName(), &snapst))

	var candidates map[string]*refreshCandidate
	if mylog.Check(st.Get("refresh-candidates", &candidates)); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
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

	hasRefreshControl := mylog.Check2(hasSnapRefreshControlInterface(st, context.InstanceName()))

	if hasRefreshControl {
		up.CohortKey = snapst.CohortKey
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
	details := mylog.Check2(getUpdateDetails(c.context()))

	out := mylog.Check2(yaml.Marshal(details))

	c.printf("%s", string(out))
	return nil
}

func (c *refreshCommand) hold() error {
	ctx := c.context()
	if ctx.IsEphemeral() {
		return fmt.Errorf("cannot hold outside of gate-auto-refresh hook")
	}
	ctx.Lock()
	defer ctx.Unlock()
	st := ctx.State()

	// cache the action so that hook handler can implement default behavior
	ctx.Cache("action", snapstate.GateAutoRefreshHold)

	affecting := mylog.Check2(snapstate.AffectingSnapsForAffectedByRefreshCandidates(st, ctx.InstanceName()))

	if len(affecting) == 0 {
		// this shouldn't happen because the hook is executed during auto-refresh
		// change which conflicts with other changes (if it happens that means
		// something changed in the meantime and we didn't handle conflicts
		// correctly).
		return fmt.Errorf("internal error: snap %q is not affected by any snaps", ctx.InstanceName())
	}

	// no duration specified, use maximum allowed for this gating snap.
	var holdDuration time.Duration
	// XXX for now snaps hold other snaps only for auto-refreshes
	remaining := mylog.Check2(snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, ctx.InstanceName(), holdDuration, affecting...))

	// TODO: let a snap hold again once for 1h.

	var details holdDetails
	details.Hold = remaining.String()

	out := mylog.Check2(yaml.Marshal(details))

	c.printf("%s", string(out))

	return nil
}

func (c *refreshCommand) proceed() error {
	ctx := c.context()
	ctx.Lock()
	defer ctx.Unlock()

	// running outside of hook
	if ctx.IsEphemeral() {
		st := ctx.State()
		hasRefreshControl := mylog.Check2(hasSnapRefreshControlInterface(st, ctx.InstanceName()))

		if !hasRefreshControl {
			return fmt.Errorf("cannot proceed: requires snap-refresh-control interface")
		}
		// we need to check if GateAutoRefreshHook feature is enabled when
		// running by the snap (we don't need to do this when running from the
		// hook because in that case hook task won't be created if not enabled).
		tr := config.NewTransaction(st)
		gateAutoRefreshHook := mylog.Check2(features.Flag(tr, features.GateAutoRefreshHook))
		if err != nil && !config.IsNoOption(err) {
			return err
		}
		if !gateAutoRefreshHook {
			return fmt.Errorf("cannot proceed without experimental.gate-auto-refresh feature enabled")
		}

		return autoRefreshForGatingSnap(st, ctx.InstanceName())
	}

	// cache the action, hook handler will trigger proceed logic; we cannot
	// call snapstate.ProceedWithRefresh() immediately as this would reset
	// holdState, allowing the snap to --hold with fresh duration limit.
	ctx.Cache("action", snapstate.GateAutoRefreshProceed)

	return nil
}

func hasSnapRefreshControlInterface(st *state.State, snapName string) (bool, error) {
	conns := mylog.Check2(ifacestate.ConnectionStates(st))

	for refStr, connState := range conns {
		if connState.Undesired || connState.Interface != "snap-refresh-control" {
			continue
		}
		connRef := mylog.Check2(interfaces.ParseConnRef(refStr))

		if connRef.PlugRef.Snap == snapName {
			return true, nil
		}
	}
	return false, nil
}

func (c *refreshCommand) printInhibitLockHint() error {
	ctx := c.context()
	ctx.Lock()
	snapName := ctx.InstanceName()
	ctx.Unlock()

	// obtain snap lock before manipulating runinhibit lock.
	lock := mylog.Check2(snaplock.OpenLock(snapName))
	mylog.Check(lock.Lock())

	defer lock.Unlock()

	hint, _ := mylog.Check3(runinhibit.IsLocked(snapName))

	c.printf("%s", hint)
	return nil
}
