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
	"github.com/snapcore/snapd/overlord/hookstate"
	"regexp"
)

type collectAttrHandler struct {
}

func (h *collectAttrHandler) Before() error {
	return nil
}

func (h *collectAttrHandler) Done() error {
	return nil
}

func (h *collectAttrHandler) Error(err error) error {
	return nil
}

func SetupHooks(hookMgr *hookstate.HookManager) {
	generator := func(context *hookstate.Context) hookstate.Handler {
		return &collectAttrHandler{}
	}

	hookMgr.Register(regexp.MustCompile("^collect-plug-attr-[a-zA-Z0-9_]+$"), generator)
	hookMgr.Register(regexp.MustCompile("^collect-slot-attr-[a-zA-Z0-9_]+$"), generator)
}
