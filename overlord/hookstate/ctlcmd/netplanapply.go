// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/cmdstate"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var shortNetplanApplyHelp = i18n.G("The netplan-apply command applies network configuration from netplan.")
var longNetplanApplyHelp = i18n.G("TODO")

func init() {
	addCommand("netplan-apply", shortNetplanApplyHelp, longNetplanApplyHelp, func() command {
		return &netplanApplyCommand{}
	})
}

type netplanApplyCommand struct {
	baseCommand
}

// Execute runs the netplan-apply command after confirming the calling snap has
// permissions to run "netplan apply" with network-setup-control connected and
// the netplan-apply attribute specified on the plug
func (c *netplanApplyCommand) Execute(args []string) error {
	ctx := c.context()
	if ctx == nil {
		return errors.New("missing snapctl context")
	}

	// check if netplan apply can be used with this context
	canUse, err := canUseNetplanApply(ctx)
	if err != nil {
		return err
	}
	if !canUse {
		// TODO: is there a better error type to return here?
		return fmt.Errorf("cannot use netplan apply - must have network-setup-control interface connected with netplan-apply attribute specified as true")
	}

	// note: don't lock state here, we lock/unlock it inside netplanApplyTaskSet
	st := ctx.State()
	if st == nil {
		return errors.New("context state is nil")
	}

	// create new netplan apply task set
	tts, err := netplanApplyTaskSet(st, ctx)
	if err != nil {
		return err
	}

	// lock the state and create a new change for the task
	st.Lock()
	chg := st.NewChange("netplan-apply", fmt.Sprintf("Applying netplan network configuration for snap %q", ctx.InstanceName()))
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)
	st.Unlock()

	// wait for the change to be ready or for it to timeout
	select {
	case <-chg.Ready():
		st.Lock()
		defer st.Unlock()
		return chg.Err()
	case <-time.After(configstate.ConfigureHookTimeout() / 2):
		return errors.New("netplan apply command is taking too long")
	}
}

func canUseNetplanApply(ctx *hookstate.Context) (bool, error) {
	// note: the following is adapted from devicestate.CanManageRefreshes
	st := ctx.State()
	if st == nil {
		return false, errors.New("context state is nil")
	}

	// lock the state and get all snap states
	st.Lock()
	defer st.Unlock()
	snapStates, err := snapstate.All(ctx.State())
	if err != nil {
		return false, err
	}

	// check each snap state for plugs to see if the network-setup-control
	// interface is connected with netplan-apply as true
	for _, snapst := range snapStates {
		// Always get the current info even if the snap is currently
		// being operated on or if its disabled.
		info, err := snapst.CurrentInfo()
		if err != nil {
			continue
		}
		if info.Broken != "" {
			continue
		}

		// TODO: should a check for a snap snap declaration from the store be
		// here too?

		for _, plugInfo := range info.Plugs {
			if plugInfo.Interface == "network-setup-control" {
				attrVal, ok := plugInfo.Attrs["netplan-apply"]
				if !ok {
					return false, nil
				}
				switch attrVal {
				case "true":
					conns, err := ifacerepo.Get(st).Connected(info.InstanceName(), plugInfo.Name)
					if err != nil {
						continue
					}
					if len(conns) > 0 {
						// it's connected
						return true, nil
					}
				case "false", "":
					// TODO: is it valid to have multiple versions of the
					// interface connected, one with false and another with
					// true? probably not so fail fast here...
					return false, nil
				default:
					return false, errors.New("invalid setting for netplan-apply, must be true/false")
				}
			}
		}
	}

	// didn't find the interface connection, can't use netplan apply
	return false, nil
}

func netplanApplyTaskSet(st *state.State, ctx *hookstate.Context) ([]*state.TaskSet, error) {
	var tts []*state.TaskSet

	st.Lock()
	defer st.Unlock()

	// TODO: should we check for conflicts?

	argv := []string{"/usr/sbin/netplan", "apply"}
	// give netplan 15 seconds to execute
	// TODO: time netplan apply on slow devices
	ts := cmdstate.ExecWithTimeout(st, "netplan apply", argv, 15*time.Second)
	tts = append(tts, ts)

	// make a taskset wait for its predecessor
	for i := 1; i < len(tts); i++ {
		tts[i].WaitAll(tts[i-1])
	}

	return tts, nil
}
