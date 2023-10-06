// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package configstate implements the manager and state aspects responsible for
// the configuration of snaps.
package configstate

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	snapstate.Configure = Configure
	snapstate.DefaultConfigure = DefaultConfigure
}

func ConfigureHookTimeout() time.Duration {
	timeout := 5 * time.Minute
	if s := os.Getenv("SNAPD_CONFIGURE_HOOK_TIMEOUT"); s != "" {
		if to, err := time.ParseDuration(s); err == nil {
			timeout = to
		}
	}
	return timeout
}

func canConfigure(st *state.State, snapName string) error {
	// the "core" snap/pseudonym can always be configured as it
	// is handled internally
	if snapName == "core" {
		return nil
	}

	var snapst snapstate.SnapState
	err := snapstate.Get(st, snapName, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if !snapst.IsInstalled() {
		return &snap.NotInstalledError{Snap: snapName}
	}

	// the "snapd" snap cannot be configured yet
	typ, err := snapst.Type()
	if err != nil {
		return err
	}
	if typ == snap.TypeSnapd {
		return fmt.Errorf(`cannot configure the "snapd" snap, please use "system" instead`)
	}

	// bases cannot be configured for now
	typ, err = snapst.Type()
	if err != nil {
		return err
	}
	if typ == snap.TypeBase {
		return fmt.Errorf("cannot configure snap %q because it is of type 'base'", snapName)
	}

	return snapstate.CheckChangeConflict(st, snapName, nil)
}

// ConfigureInstalled returns a taskset to apply the given
// configuration patch for an installed snap. It returns
// snap.NotInstalledError if the snap is not installed.
func ConfigureInstalled(st *state.State, snapName string, patch map[string]interface{}, flags int) (*state.TaskSet, error) {
	if err := canConfigure(st, snapName); err != nil {
		return nil, err
	}

	taskset := Configure(st, snapName, patch, flags)
	return taskset, nil
}

// Configure returns a taskset to apply the given configuration patch.
func Configure(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
	summary := fmt.Sprintf(i18n.G("Run configure hook of %q snap"), snapName)
	// regular configuration hook
	hooksup := &hookstate.HookSetup{
		Snap:        snapName,
		Hook:        "configure",
		Optional:    len(patch) == 0,
		IgnoreError: flags&snapstate.IgnoreHookError != 0,
		// all configure hooks must finish within this timeout
		Timeout: ConfigureHookTimeout(),
	}
	var contextData map[string]interface{}
	if flags&snapstate.UseConfigDefaults != 0 {
		contextData = map[string]interface{}{"use-defaults": true}
	} else if len(patch) > 0 {
		contextData = map[string]interface{}{"patch": patch}
	}

	if hooksup.Optional {
		summary = fmt.Sprintf(i18n.G("Run configure hook of %q snap if present"), snapName)
	}

	task := hookstate.HookTask(st, summary, hooksup, contextData)
	return state.NewTaskSet(task)
}

// DefaultConfigure returns a taskset to apply the given default-configuration patch.
func DefaultConfigure(st *state.State, snapName string) *state.TaskSet {
	summary := fmt.Sprintf(i18n.G("Run default-configure hook of %q snap if present"), snapName)
	hooksup := &hookstate.HookSetup{
		Snap:     snapName,
		Hook:     "default-configure",
		Optional: true,
		// all configure hooks must finish within this timeout
		Timeout: ConfigureHookTimeout(),
	}
	// the default-configure hook always uses defaults, no need to indicate this
	// by setting use-defaults flag in context data
	task := hookstate.HookTask(st, summary, hooksup, nil)
	return state.NewTaskSet(task)
}

// RemapSnapFromRequest renames a snap as received from an API request
func RemapSnapFromRequest(snapName string) string {
	if snapName == "system" {
		return "core"
	}
	return snapName
}

// RemapSnapToResponse renames a snap as about to be sent from an API response
func RemapSnapToResponse(snapName string) string {
	if snapName == "core" {
		return "system"
	}
	return snapName
}

func delayedCrossMgrInit() {
	devicestate.EarlyConfig = EarlyConfig
}

var (
	configcoreExportExperimentalFlags = configcore.ExportExperimentalFlags
	configcoreEarly                   = configcore.Early
)

// EarlyConfig performs any needed early configuration handling during
// managers' startup, it is exposed as a hook to devicestate for invocation.
// preloadGadget if set will be invoked if the system is not yet seeded
// or configured, it should either return ErrNoState, or return
// the gadget.Info for the to-be-seeded gadget and details about
// the model/device as sysconfig.Device.
func EarlyConfig(st *state.State, preloadGadget func() (sysconfig.Device, *gadget.Info, error)) error {
	// already configured
	configed, err := systemAlreadyConfigured(st)
	if err != nil {
		return err
	}
	// No task is associated to the transaction if it is an early config
	rt := configcore.NewRunTransaction(config.NewTransaction(st), nil)
	if configed {
		if err := configcoreExportExperimentalFlags(rt); err != nil {
			return fmt.Errorf("cannot export experimental config flags: %v", err)
		}
		return nil
	}
	if preloadGadget != nil {
		dev, gi, err := preloadGadget()
		if err != nil {
			if errors.Is(err, state.ErrNoState) {
				// nothing to do
				return nil
			}
			return err
		}
		values := gadget.SystemDefaults(gi.Defaults)
		if err := configcoreEarly(dev, rt, values); err != nil {
			return err
		}
		rt.Commit()
	}
	return nil
}

func systemAlreadyConfigured(st *state.State) (bool, error) {
	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}
	if seeded {
		return true, nil
	}
	cfg, err := config.GetSnapConfig(st, "core")
	if cfg != nil {
		return true, nil
	}
	return false, err
}
