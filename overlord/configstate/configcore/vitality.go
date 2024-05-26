// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configcore

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

const vitalityOpt = "resilience.vitality-hint"

func init() {
	// add supported configuration of this module
	supportedConfigurations["core."+vitalityOpt] = true
}

func handleVitalityConfiguration(tr RunTransaction, opts *fsOnlyContext) error {
	var pristineVitalityStr, newVitalityStr string

	if mylog.Check(tr.GetPristine("core", vitalityOpt, &pristineVitalityStr)); err != nil && !config.IsNoOption(err) {
		return err
	}
	if mylog.Check(tr.Get("core", vitalityOpt, &newVitalityStr)); err != nil && !config.IsNoOption(err) {
		return err
	}
	if pristineVitalityStr == newVitalityStr {
		return nil
	}

	st := tr.State()
	st.Lock()
	defer st.Unlock()

	// TODO: Reimplement most of this as a servicestate.UpdateVitalityRank

	oldVitalityMap := map[string]int{}
	newVitalityMap := map[string]int{}
	// assign "0" (delete) rank to all old entries
	for i, instanceName := range strings.Split(pristineVitalityStr, ",") {
		oldVitalityMap[instanceName] = i + 1
		newVitalityMap[instanceName] = 0
	}
	// build rank of the new entries
	for i, instanceName := range strings.Split(newVitalityStr, ",") {
		newVitalityMap[instanceName] = i + 1
	}

	// use a single cache of the quota groups for calculating the quota groups
	// that services should be in
	grps := mylog.Check2(servicestate.AllQuotas(st))

	for instanceName, rank := range newVitalityMap {
		var snapst snapstate.SnapState
		mylog.Check(snapstate.Get(st, instanceName, &snapst))
		// not installed, vitality-score will be applied when the snap
		// gets installed
		if errors.Is(err, state.ErrNoState) {
			continue
		}

		// not active, vitality-score will be applied when the snap
		// becomes active
		if !snapst.Active {
			continue
		}
		info := mylog.Check2(snapst.CurrentInfo())

		// nothing to do if rank is unchanged
		if oldVitalityMap[instanceName] == newVitalityMap[instanceName] {
			continue
		}

		// rank changed, rewrite/restart services

		// first get the device context to decide if we need to set
		// RequireMountedSnapdSnap
		// TODO: use sysconfig.Device instead
		deviceCtx := mylog.Check2(snapstate.DeviceCtx(st, nil, nil))

		ensureOpts := &wrappers.EnsureSnapServicesOptions{}

		// we need the snapd snap mounted whenever in order for services to
		// start for all services on UC18+
		if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
			ensureOpts.RequireMountedSnapdSnap = true
		}

		// get the options for this snap service
		snapSvcOpts := mylog.Check2(servicestate.SnapServiceOptions(st, info, grps))

		m := map[*snap.Info]*wrappers.SnapServiceOptions{
			info: snapSvcOpts,
		}

		// overwrite the VitalityRank we got from SnapServiceOptions to use the
		// rank we calculated as part of this transaction
		m[info].VitalityRank = rank
		mylog.Check(

			// ensure that the snap services are re-written with these units
			wrappers.EnsureSnapServices(m, ensureOpts, nil, progress.Null))

		// and then restart the services

		// TODO: this doesn't actually restart the services, meaning that the
		// OOMScoreAdjust vitality-hint ranking doesn't take effect until the
		// service is restarted by a refresh or a reboot, etc.
		// TODO: this option also doesn't work with services that use
		// Delegate=true, i.e. docker, greengrass, kubernetes, so we should do
		// something about that combination because currently we jus silently
		// apply the setting which never does anything

		// XXX: copied from handlers.go:startSnapServices()

		disabledSvcs := mylog.Check2(wrappers.QueryDisabledServices(info, progress.Null))

		svcs := info.Services()
		startupOrdered := mylog.Check2(snap.SortServices(svcs))

		flags := &wrappers.StartServicesFlags{Enable: true}
		tm := timings.New(nil)
		mylog.Check(wrappers.StartServices(startupOrdered, disabledSvcs, flags, progress.Null, tm))

	}

	return nil
}

func validateVitalitySettings(tr RunTransaction) error {
	option := mylog.Check2(coreCfg(tr, vitalityOpt))

	if option == "" {
		return nil
	}
	vitalityHints := strings.Split(option, ",")
	if len(vitalityHints) > 100 {
		return fmt.Errorf("cannot set more than 100 snaps in %q: got %v", vitalityOpt, len(vitalityHints))
	}
	for _, instanceName := range vitalityHints {
		mylog.Check(naming.ValidateInstance(instanceName))

		// The "snapd" snap is always at OOMScoreAdjust=-900.
		if instanceName == "snapd" {
			return fmt.Errorf("cannot set %q: snapd snap vitality cannot be changed", vitalityOpt)
		}
	}

	return nil
}
