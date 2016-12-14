// -*- Mode: Go; indent-tabs-mode: t -*-

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

package ifacestate

import (
	"regexp"

	"github.com/snapcore/snapd/overlord/hookstate"
)

type prepareHandler struct{}

func (h *prepareHandler) Before() error {
	return nil
}

func (h *prepareHandler) Done() error {
	return nil
}

func (h *prepareHandler) Error(err error) error {
	return nil
}

// setupHooks sets hooks of InterfaceManager up
func setupHooks(hookMgr *hookstate.HookManager) {
	prepPlugGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &prepareHandler{}
	}

	prepSlotGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &prepareHandler{}
	}

	hookMgr.Register(regexp.MustCompile("^prepare-plug-[a-zA-Z0-9_\\-]+$"), prepPlugGenerator)
	hookMgr.Register(regexp.MustCompile("^prepare-slot-[a-zA-Z0-9_\\-]+$"), prepSlotGenerator)
}
