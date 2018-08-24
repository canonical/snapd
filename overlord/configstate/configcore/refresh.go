// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"strconv"
	"time"

	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
)

func init() {
	supportedConfigurations["core.refresh.hold"] = true
	supportedConfigurations["core.refresh.schedule"] = true
	supportedConfigurations["core.refresh.timer"] = true
	supportedConfigurations["core.refresh.metered"] = true
	supportedConfigurations["core.refresh.retain"] = true
	supportedConfigurations["core.refresh.rate-limit"] = true
}

func validateRefreshSchedule(tr Conf) error {
	refreshRetainStr, err := coreCfg(tr, "refresh.retain")
	if err != nil {
		return err
	}
	if refreshRetainStr != "" {
		if n, err := strconv.ParseUint(refreshRetainStr, 10, 8); err != nil || (n < 2 || n > 20) {
			return fmt.Errorf("retain must be a number between 2 and 20, not %q", refreshRetainStr)
		}
	}

	refreshHoldStr, err := coreCfg(tr, "refresh.hold")
	if err != nil {
		return err
	}
	if refreshHoldStr != "" {
		if _, err := time.Parse(time.RFC3339, refreshHoldStr); err != nil {
			return fmt.Errorf("refresh.hold cannot be parsed: %v", err)
		}
	}

	refreshOnMeteredStr, err := coreCfg(tr, "refresh.metered")
	if err != nil {
		return err
	}
	switch refreshOnMeteredStr {
	case "", "hold":
		// noop
	default:
		return fmt.Errorf("refresh.metered value %q is invalid", refreshOnMeteredStr)
	}

	// check (new) refresh.timer
	refreshTimerStr, err := coreCfg(tr, "refresh.timer")
	if err != nil {
		return err
	}
	if refreshTimerStr == "managed" {
		st := tr.State()
		st.Lock()
		defer st.Unlock()

		if !devicestate.CanManageRefreshes(st) {
			return fmt.Errorf("cannot set schedule to managed")
		}
		return nil
	}
	if refreshTimerStr != "" {
		// try legacy refresh.schedule setting if new-style
		// refresh.timer is not set
		if _, err = timeutil.ParseSchedule(refreshTimerStr); err != nil {
			return err
		}
	}

	// check (legacy) refresh.schedule
	refreshScheduleStr, err := coreCfg(tr, "refresh.schedule")
	if err != nil {
		return err
	}
	if refreshScheduleStr == "" {
		return nil
	}

	if refreshScheduleStr == "managed" {
		st := tr.State()
		st.Lock()
		defer st.Unlock()

		if !devicestate.CanManageRefreshes(st) {
			return fmt.Errorf("cannot set schedule to managed")
		}
		return nil
	}

	_, err = timeutil.ParseLegacySchedule(refreshScheduleStr)
	return err
}

func validateRefreshRateLimit(tr Conf) error {
	refreshRateLimit, err := coreCfg(tr, "refresh.rate-limit")
	if err != nil {
		return err
	}
	// reset is fine
	if len(refreshRateLimit) == 0 {
		return nil
	}
	if _, err := strutil.ParseValueWithUnit(refreshRateLimit); err != nil {
		return err
	}
	return nil
}
