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

type prepareHandler struct {
	context *hookstate.Context
}

type connectHandler struct {
	context *hookstate.Context
}

func (h *prepareHandler) Before() error {
	return nil
}

func (h *prepareHandler) Done() error {
	return nil
}

func (h *prepareHandler) Error(err error) error {
	return nil
}

func (h *connectHandler) Before() error {
	return nil
}

func (h *connectHandler) Done() error {
	return nil
}

func (h *connectHandler) Error(err error) error {
	return nil
}

// setupHooks sets hooks of InterfaceManager up
func setupHooks(hookMgr *hookstate.HookManager) {
	prepareGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &prepareHandler{context: context}
	}

	connectGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &connectHandler{context: context}
	}

	hookMgr.Register(regexp.MustCompile("^prepare-plug-[-a-z0-9]+$"), prepareGenerator)
	hookMgr.Register(regexp.MustCompile("^prepare-slot-[-a-z0-9]+$"), prepareGenerator)
	hookMgr.Register(regexp.MustCompile("^connect-plug-[-a-z0-9]+$"), connectGenerator)
	hookMgr.Register(regexp.MustCompile("^connect-slot-[-a-z0-9]+$"), connectGenerator)
}
