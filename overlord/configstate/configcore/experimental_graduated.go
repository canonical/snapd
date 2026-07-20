// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2026 Canonical Ltd
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

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

func isGraduatedExperimentalChange(k string) bool {
	if !strings.HasPrefix(k, "core.experimental.") {
		return false
	}
	return features.IsGraduated(strings.TrimPrefix(k, "core.experimental."))
}

func isDefaultEnabledExperimentalChange(k string) bool {
	if !strings.HasPrefix(k, "core.experimental.") {
		return false
	}
	featureName := strings.TrimPrefix(k, "core.experimental.")

	for _, feature := range features.KnownFeatures() {
		if feature.String() == featureName {
			return feature.IsEnabledWhenUnset()
		}
	}
	return false
}

func warnDefaultEnabledExperimentalChange(cfg RunTransaction, k string) error {
	if !strings.HasPrefix(k, "core.experimental.") {
		return errors.New("internal error: change is not an experimental feature")
	}
	feature := strings.TrimPrefix(k, "core.experimental.")

	// send log to the task logs and and the warnings system
	msg := fmt.Sprintf("feature %s is enabled by default and will be permanently enabled in a future release", feature)
	warnExperimentalChange(cfg, msg)

	return nil
}

func dropGraduatedExperimentalChange(cfg RunTransaction, k string) error {
	if !strings.HasPrefix(k, "core.experimental.") {
		return errors.New("internal error: change is not an experimental feature")
	}
	feature := strings.TrimPrefix(k, "core.experimental.")

	// setting to nil here drops the flag from the state. it should have already
	// been cleared out by configstate.Init, but doing it here should not hurt.
	if err := cfg.Set("core", "experimental."+feature, nil); err != nil {
		return err
	}

	// send log to the journal, task logs, and and the warnings system
	msg := fmt.Sprintf("feature %s is no longer experimental and is always enabled", feature)

	logger.Noticef("%s", msg)
	warnExperimentalChange(cfg, msg)

	return nil
}

// PruneGraduatedExperimentalConfig removes persisted experimental settings for
// features that graduated and are now always enabled.
func PruneGraduatedExperimentalConfig(cfg RunTransaction) error {
	for _, feature := range features.Graduated() {
		var value any
		err := cfg.Get("core", "experimental."+feature, &value)
		if config.IsNoOption(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := cfg.Set("core", "experimental."+feature, nil); err != nil {
			return err
		}
	}
	return nil
}

func warnExperimentalChange(cfg RunTransaction, msg string) {
	st := cfg.State()
	st.Lock()
	defer st.Unlock()

	if task := cfg.Task(); task != nil {
		task.Logf("%s", msg)
	}
	st.Warnf("%s", msg)
}
