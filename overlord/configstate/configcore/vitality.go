// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
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

func isNotInstalled(err error) bool {
	_, ok := err.(*snap.NotInstalledError)
	return ok
}

func handleVitalityConfiguration(tr config.Conf, opts *fsOnlyContext) error {
	var pristineVitalityStr, newVitalityStr string

	if err := tr.GetPristine("core", vitalityOpt, &pristineVitalityStr); err != nil && !config.IsNoOption(err) {
		return err
	}
	if err := tr.Get("core", vitalityOpt, &newVitalityStr); err != nil && !config.IsNoOption(err) {
		return err
	}
	if pristineVitalityStr == newVitalityStr {
		return nil
	}

	st := tr.State()
	st.Lock()
	defer st.Unlock()

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

	for instanceName, rank := range newVitalityMap {
		info, err := snapstate.CurrentInfo(st, instanceName)
		if isNotInstalled(err) {
			continue
		}
		if err != nil {
			return err
		}
		// nothing rank to do if rank is unchanged
		if oldVitalityMap[instanceName] == newVitalityMap[instanceName] {
			continue
		}
		// rank changed, rewrite/restart services
		for _, app := range info.Apps {
			if !app.IsService() {
				continue
			}
			opts := &wrappers.AddSnapServicesOptions{VitalityRank: rank}
			if err := wrappers.AddSnapServices(info, nil, opts, progress.Null); err != nil {
				return err
			}
		}
		// XXX: copied from handlers.go:startSnapServices()
		svcs := info.Services()
		startupOrdered, err := snap.SortServices(svcs)
		if err != nil {
			return err
		}
		tm := timings.New(nil)
		pb := progress.Null
		if err = wrappers.StartServices(startupOrdered, pb, tm); err != nil {
			return err
		}
	}

	return nil
}

func validateVitalitySettings(tr config.Conf) error {
	option, err := coreCfg(tr, vitalityOpt)
	if err != nil {
		return err
	}
	if option == "" {
		return nil
	}
	vitalityHints := strings.Split(option, ",")
	if len(vitalityHints) > 100 {
		return fmt.Errorf("cannot set more than 100 snaps in %q: got %v", vitalityOpt, len(vitalityHints))
	}
	for _, instanceName := range vitalityHints {
		if err := naming.ValidateInstance(instanceName); err != nil {
			return fmt.Errorf("cannot set %q: %v", vitalityOpt, err)
		}
	}

	return nil
}
