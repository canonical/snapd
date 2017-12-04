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
	"regexp"

	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/hookstate"
)

var configcoreRun = configcore.Run

func MockConfigcoreRun(f func(conf configcore.Conf) error) (restore func()) {
	origConfigcoreRun := configcoreRun
	configcoreRun = f
	return func() {
		configcoreRun = origConfigcoreRun
	}
}

func Init(hookManager *hookstate.HookManager) {
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
}
