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
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
)

const (
	minInhibitionDays = 1
	maxInhibitionDays = 21
)

func init() {
	supportedConfigurations["core.refresh.hold"] = true
	supportedConfigurations["core.refresh.schedule"] = true
	supportedConfigurations["core.refresh.timer"] = true
	supportedConfigurations["core.refresh.metered"] = true
	supportedConfigurations["core.refresh.retain"] = true
	supportedConfigurations["core.refresh.rate-limit"] = true
	supportedConfigurations["core.refresh.delta-formats"] = true
	supportedConfigurations["core.refresh.max-inhibition-days"] = true
}

func reportOrIgnoreInvalidManageRefreshes(tr RunTransaction, optName string) error {
	// check if the option is set as part of transaction changes; if not
	// than it's already set in the config state and we shouldn't error out
	// about it now since the required conditions were met at the time it
	// got set
	for _, k := range tr.Changes() {
		if k == "core."+optName {
			return fmt.Errorf("cannot set schedule to managed")
		}
	}
	return nil
}

func validateRefreshSchedule(tr RunTransaction) error {
	maxInhibitionDaysStr, err := coreCfg(tr, "refresh.max-inhibition-days")
	if err != nil {
		return err
	}
	if maxInhibitionDaysStr != "" {
		if n, err := strconv.ParseUint(maxInhibitionDaysStr, 10, 8); err != nil || (n < minInhibitionDays || n > maxInhibitionDays) {
			return fmt.Errorf("max-inhibition-days must be a number between %d and %d, not %q", minInhibitionDays, maxInhibitionDays, maxInhibitionDaysStr)
		}
	}
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
	if refreshHoldStr != "" && refreshHoldStr != "forever" {
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

		// ensure there is a snap entitled to managing the refreshes
		// in an autonomic manner
		if !devicestate.CanManageRefreshes(st) {
			return reportOrIgnoreInvalidManageRefreshes(tr, "refresh.timer")
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

		// see the comment around refresh.timer
		if !devicestate.CanManageRefreshes(st) {
			return reportOrIgnoreInvalidManageRefreshes(tr, "refresh.schedule")
		}
		return nil
	}

	_, err = timeutil.ParseLegacySchedule(refreshScheduleStr)
	return err
}

func validateRefreshRateLimit(tr RunTransaction) error {
	refreshRateLimit, err := coreCfg(tr, "refresh.rate-limit")
	if err != nil {
		return err
	}
	// reset is fine
	if len(refreshRateLimit) == 0 {
		return nil
	}

	if _, err := strutil.ParseByteSize(refreshRateLimit); err != nil {
		return err
	}
	return nil
}

func validateRefreshDeltaFormats(tr RunTransaction) error {
	if !strutil.ListContains(tr.Changes(), "core.refresh.delta-formats") {
		return nil
	}
	var formats []string
	if err := tr.GetMaybe("core", "refresh.delta-formats", &formats); err != nil {
		return err
	}
	if formats == nil {
		return nil
	}
	return snapstate.ValidateRefreshDeltaFormats(formats)
}

// MigrateRefreshDeltaFormats seeds refresh.delta-formats from the legacy
// experimental flag, preserving the flag for rollback and any configured value.
// The caller must hold the state lock.
func MigrateRefreshDeltaFormats(tr RunTransaction) error {
	var migrated bool
	if err := tr.State().Get("refresh-delta-formats-migrated", &migrated); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if migrated {
		return nil
	}

	var value any
	err := tr.Get("core", "refresh.delta-formats", &value)
	if err == nil {
		return nil
	}
	if !config.IsNoOption(err) {
		return err
	}
	var flag any
	if err := tr.GetMaybe("core", "experimental.snap-delta-format", &flag); err != nil {
		return err
	}
	switch flag {
	case true, "true":
		return tr.Set("core", "refresh.delta-formats", []string{"snap-1-1-xdelta3", "xdelta3"})
	case false, "false":
		return tr.Set("core", "refresh.delta-formats", []string{"xdelta3"})
	case nil, "":
		return nil
	default:
		logger.Noticef("cannot migrate invalid experimental.snap-delta-format value %v", flag)
		return nil
	}
}

func earlyRefreshDeltaFormatsFilter(values, early map[string]any) {
	for _, key := range []string{"refresh.delta-formats", "experimental.snap-delta-format"} {
		if value, ok := values[key]; ok {
			early[key] = value
		}
	}
}

// handleLegacySnapDeltaFormat migrates a legacy flag toggled at runtime unless
// the transaction explicitly sets or unsets refresh.delta-formats.
func handleLegacySnapDeltaFormat(tr RunTransaction, _ *fsOnlyContext) error {
	if strutil.ListContains(tr.Changes(), "core.refresh.delta-formats") ||
		!strutil.ListContains(tr.Changes(), "core.experimental.snap-delta-format") {
		return nil
	}
	st := tr.State()
	st.Lock()
	defer st.Unlock()
	return MigrateRefreshDeltaFormats(tr)
}
