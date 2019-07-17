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

var shortNetplanApplyHelp = i18n.G("The netplan-apply command applies network configuration via netplan.")
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

	// note: don't lock state here, we lock/unlock it inside netplanApplyTaskSet
	// and inside canUseNetplanApply
	st := ctx.State()
	if st == nil {
		return errors.New("context state is nil")
	}

	// check if netplan apply can be used with this context
	canUse, err := canUseNetplanApply(st, ctx.InstanceName())
	if err != nil {
		return err
	}
	if !canUse {
		// TODO: is there a better error type to return here?
		return fmt.Errorf("cannot use netplan apply - must have network-setup-control interface connected with netplan-apply attribute specified as true")
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

func canUseNetplanApply(st *state.State, instanceName string) (bool, error) {
	// note: the following is adapted from devicestate.CanManageRefreshes
	if st == nil {
		return false, errors.New("context state is nil")
	}

	// lock the state and get all snap states
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, instanceName, &snapst); err != nil {
		return false, err
	}

	// Always get the current info even if the snap is currently
	// being operated on or if its disabled.
	info, err := snapst.CurrentInfo()
	if err != nil {
		return false, err
	}
	if info.Broken != "" {
		return false, err
	}

	// TODO: should a check for a snap declaration from the store be here too?

	for _, plugInfo := range info.Plugs {
		if plugInfo.Interface == "network-setup-control" {
			if v, ok := plugInfo.Attrs["netplan-apply"]; ok {
				var netplanApply bool
				if netplanApply, ok = v.(bool); !ok {
					return false, errors.New("network-setup-control plug requires bool with 'netplan-apply'")
				}
				if netplanApply {
					conns, err := ifacerepo.Get(st).Connected(info.InstanceName(), plugInfo.Name)
					if err != nil {
						// TODO: how could we handle or notify about this error?
						continue
					}
					if len(conns) > 0 {
						// it's connected
						return true, nil
					}
				}
			}

			// TODO: is it valid to have multiple versions of the
			// interface connected, one with false and another with
			// true? probably not so fail fast here...
			return false, nil
		}
	}

	// didn't find the interface connection, can't use netplan apply
	return false, nil
}

func netplanApplyTaskSet(st *state.State, ctx *hookstate.Context) ([]*state.TaskSet, error) {
	st.Lock()
	defer st.Unlock()

	argv := []string{"netplan", "apply"}
	// give netplan 15 seconds to execute
	// TODO: time netplan apply on slow devices
	ts := cmdstate.ExecWithTimeout(st, "netplan apply", argv, 15*time.Second)

	return []*state.TaskSet{ts}, nil
}
