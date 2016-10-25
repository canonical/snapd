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

// Package ifacestate implements the manager and state aspects
// responsible for the maintenance of interfaces the system.
package ifacestate

import (
	"regexp"

	"github.com/snapcore/snapd/overlord/hookstate"
)

type collectAttrHandler struct{}

type confirmConnectionHandler struct{}

func (h *collectAttrHandler) Before() error {
	return nil
}

func (h *collectAttrHandler) Done() error {
	return nil
}

func (h *collectAttrHandler) Error(err error) error {
	return nil
}

func (h *confirmConnectionHandler) Before() error {
	return nil
}

func (h *confirmConnectionHandler) Done() error {
	return nil
}

func (h *confirmConnectionHandler) Error(err error) error {
	return nil
}

// SetupHooks sets hooks of InterfaceManager up
func setupHooks(hookMgr *hookstate.HookManager) {
	prepPlugGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &collectAttrHandler{}
	}

	prepSlotGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &collectAttrHandler{}
	}

	confirmPlugGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &confirmConnectionHandler{}
	}

	confirmSlotGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &confirmConnectionHandler{}
	}

	hookMgr.Register(regexp.MustCompile("^prepare-plug-[a-zA-Z0-9_\\-]+$"), prepPlugGenerator)
	hookMgr.Register(regexp.MustCompile("^prepare-slot-[a-zA-Z0-9_\\-]+$"), prepSlotGenerator)
	hookMgr.Register(regexp.MustCompile("^confirm-plug-[a-zA-Z0-9_\\-]+$"), confirmPlugGenerator)
	hookMgr.Register(regexp.MustCompile("^confirm-slot-[a-zA-Z0-9_\\-]+$"), confirmSlotGenerator)
}
