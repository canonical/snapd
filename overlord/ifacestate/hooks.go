// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

type interfaceHookHandler struct {
	context *hookstate.Context
}

func (h *interfaceHookHandler) Before() error {
	return nil
}

func (h *interfaceHookHandler) Done() error {
	return nil
}

func (h *interfaceHookHandler) Error(err error) error {
	return nil
}

// setupHooks sets hooks of InterfaceManager up
func setupHooks(hookMgr *hookstate.HookManager) {
	gen := func(context *hookstate.Context) hookstate.Handler {
		return &interfaceHookHandler{context: context}
	}

	hookMgr.Register(regexp.MustCompile("^prepare-plug-[-a-z0-9]+$"), gen)
	hookMgr.Register(regexp.MustCompile("^prepare-slot-[-a-z0-9]+$"), gen)
	hookMgr.Register(regexp.MustCompile("^unprepare-plug-[-a-z0-9]+$"), gen)
	hookMgr.Register(regexp.MustCompile("^unprepare-slot-[-a-z0-9]+$"), gen)
	hookMgr.Register(regexp.MustCompile("^connect-plug-[-a-z0-9]+$"), gen)
	hookMgr.Register(regexp.MustCompile("^connect-slot-[-a-z0-9]+$"), gen)
	hookMgr.Register(regexp.MustCompile("^disconnect-plug-[-a-z0-9]+$"), gen)
	hookMgr.Register(regexp.MustCompile("^disconnect-slot-[-a-z0-9]+$"), gen)
}
