// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package configstate

import (
	"fmt"
	"regexp"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

var configcoreRun = configcore.Run
var configcoreExportExperimentalFlags = configcore.ExportExperimentalFlags

func MockConfigcoreRun(f func(config.Conf) error) (restore func()) {
	origConfigcoreRun := configcoreRun
	configcoreRun = f
	return func() {
		configcoreRun = origConfigcoreRun
	}
}

func MockConfigcoreExportExperimentalFlags(mock func(tr config.Conf) error) (restore func()) {
	old := configcoreExportExperimentalFlags
	configcoreExportExperimentalFlags = mock
	return func() {
		configcoreExportExperimentalFlags = old
	}
}

func Init(st *state.State, hookManager *hookstate.HookManager) error {
	// Most configuration is handled via the "configure" hook of the
	// snaps. However some configuration is internally handled
	hookManager.Register(regexp.MustCompile("^configure$"), newConfigureHandler)
	// Ensure that we run configure for the core snap internally.
	// Note that we use the func() indirection so that mocking configcoreRun
	// in tests works correctly.
	hookManager.RegisterHijack("configure", "core", func(ctx *hookstate.Context) error {
		ctx.Lock()
		tr := ContextTransaction(ctx)
		ctx.Unlock()
		return configcoreRun(tr)
	})

	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(st)
	if err := configcoreExportExperimentalFlags(tr); err != nil {
		return fmt.Errorf("cannot export experimental config flags: %v", err)
	}
	return nil
}
