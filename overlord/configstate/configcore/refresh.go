// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2017-2022 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
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

func reportOrIgnoreInvalidManageRefreshes(tr RunTransaction, optName string) error {
	// check if the option is set as part of transaction changes; if not than
	// it's already set in the config state and we shouldn't error out about it
	// now. refreshScheduleManaged will do the right thing when refresh cannot
	// be managed anymore.
	for _, k := range tr.Changes() {
		if k == "core."+optName {
			return fmt.Errorf("cannot set schedule to managed")
		}
	}
	return nil
}

func validateRefreshSchedule(tr RunTransaction) error {
	refreshRetainStr := mylog.Check2(coreCfg(tr, "refresh.retain"))

	if refreshRetainStr != "" {
		if n := mylog.Check2(strconv.ParseUint(refreshRetainStr, 10, 8)); err != nil || (n < 2 || n > 20) {
			return fmt.Errorf("retain must be a number between 2 and 20, not %q", refreshRetainStr)
		}
	}

	refreshHoldStr := mylog.Check2(coreCfg(tr, "refresh.hold"))

	if refreshHoldStr != "" && refreshHoldStr != "forever" {
		mylog.Check2(time.Parse(time.RFC3339, refreshHoldStr))
	}

	refreshOnMeteredStr := mylog.Check2(coreCfg(tr, "refresh.metered"))

	switch refreshOnMeteredStr {
	case "", "hold":
		// noop
	default:
		return fmt.Errorf("refresh.metered value %q is invalid", refreshOnMeteredStr)
	}

	// check (new) refresh.timer
	refreshTimerStr := mylog.Check2(coreCfg(tr, "refresh.timer"))

	if refreshTimerStr == "managed" {
		st := tr.State()
		st.Lock()
		defer st.Unlock()

		if !devicestate.CanManageRefreshes(st) {
			return reportOrIgnoreInvalidManageRefreshes(tr, "refresh.timer")
		}
		return nil
	}
	if refreshTimerStr != "" {
		mylog.Check2(
			// try legacy refresh.schedule setting if new-style
			// refresh.timer is not set
			timeutil.ParseSchedule(refreshTimerStr))
	}

	// check (legacy) refresh.schedule
	refreshScheduleStr := mylog.Check2(coreCfg(tr, "refresh.schedule"))

	if refreshScheduleStr == "" {
		return nil
	}

	if refreshScheduleStr == "managed" {
		st := tr.State()
		st.Lock()
		defer st.Unlock()

		if !devicestate.CanManageRefreshes(st) {
			return reportOrIgnoreInvalidManageRefreshes(tr, "refresh.schedule")
		}
		return nil
	}

	_ = mylog.Check2(timeutil.ParseLegacySchedule(refreshScheduleStr))
	return err
}

func validateRefreshRateLimit(tr RunTransaction) error {
	refreshRateLimit := mylog.Check2(coreCfg(tr, "refresh.rate-limit"))

	// reset is fine
	if len(refreshRateLimit) == 0 {
		return nil
	}
	mylog.Check2(strutil.ParseByteSize(refreshRateLimit))

	return nil
}
