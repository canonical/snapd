// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configcore

import (
	"fmt"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

var restartRequest = restart.Request

var servicestateControl = servicestate.Control
var serviceStartChangeTimeout = time.Minute

func startHandlers(st *state.State, handlers []*snap.AppInfo) error {
	var affectedSnaps []string
	for _, h := range handlers {
		affectedSnaps = append(affectedSnaps, h.Snap.InstanceName())
	}

	// start and enable prompt handlers
	inst := &servicestate.Instruction{
		Action: "start",
		StartOptions: client.StartOptions{
			Enable: true,
		},

		Scope: []string{"user"},
	}

	st.Lock()
	// TODO hookstate? how to resolve conflicts
	tts, err := servicestateControl(st, handlers, inst, nil, &servicestate.Flags{}, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	chg := st.NewChange("service-control", fmt.Sprintf("Enable and start prompt handler services from snaps: %v", affectedSnaps))
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)
	st.Unlock()

	select {
	case <-chg.Ready():
		st.Lock()
		defer st.Unlock()
		return chg.Err()
	case <-time.After(serviceStartChangeTimeout):
		return fmt.Errorf("timeout waiting for handler services start to complete")
	}
}

func findPromptingRequestsHandlers(st *state.State) ([]*snap.AppInfo, error) {
	st.Lock()
	defer st.Unlock()

	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot get connections: %w", err)
	}

	var handlers []*snap.AppInfo

	for connId, connState := range conns {
		if connState.Interface != "snap-interfaces-requests-control" || !connState.Active() {
			continue
		}

		connRef, err := interfaces.ParseConnRef(connId)
		if err != nil {
			return nil, err
		}

		handler, ok := connState.StaticPlugAttrs["handler-service"].(string)
		if !ok {
			// does not have a handler service
			continue
		}

		sn := connRef.PlugRef.Snap
		si, err := snapstate.CurrentInfo(st, sn)
		if err != nil {
			return nil, err
		}

		// this should not fail as plug's before prepare should have validated that such app exists
		app := si.Apps[handler]
		if app == nil {
			return nil, fmt.Errorf("internal error: cannot find app %q in snap %q", app, sn)
		}

		handlers = append(handlers, app)
	}

	return handlers, nil
}

// Trigger a security profile regeneration by restarting snapd if the
// experimental apparmor-prompting flag changed.
func doExperimentalApparmorPromptingDaemonRestart(c RunTransaction, opts *fsOnlyContext) error {
	st := c.State()

	snap, confName := features.AppArmorPrompting.ConfigOption()

	var prompting bool
	err := c.Get(snap, confName, &prompting)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	var prevPrompting bool
	err = c.GetPristine(snap, confName, &prevPrompting)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if prompting == prevPrompting {
		return nil
	}

	if prompting {
		// TODO support for preseeding

		if !release.OnClassic && !release.OnCoreDesktop {
			return fmt.Errorf("cannot enable prompting feature as it is not supported on Ubuntu Core systems")
		}

		if is, whyNot := features.AppArmorPrompting.IsSupported(); !is {
			if whyNot == "" {
				// we don't have details as to why
				return fmt.Errorf("cannot enable prompting feature as it is not supported by the system")
			}
			return fmt.Errorf("cannot enable prompting feature as it is not supported by the system: %s", whyNot)
		}

		handlers, err := findPromptingRequestsHandlers(st)
		if err != nil {
			return err
		}

		if len(handlers) == 0 {
			return fmt.Errorf("cannot enable prompting feature no interfaces requests handler services are installed")
		}

		// try to start all the handlers for all active users
		if err := startHandlers(st, handlers); err != nil {
			return fmt.Errorf("cannot enable prompting, unable to start prompting handlers: %w", err)
		}
	}

	// No matter whether prompting is supported or not, request a restart of
	// snapd, since it may be the case that AppArmor has been updated and the
	// kernel or parser support for prompting has changed, and this isn't picked
	// up without re-probing the AppArmor features, which occurs during startup.

	st.Lock()
	defer st.Unlock()

	restartRequest(st, restart.RestartDaemon, nil)

	return nil
}
