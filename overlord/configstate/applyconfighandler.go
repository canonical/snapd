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

package configstate

import "github.com/snapcore/snapd/overlord/hookstate"

// applyConfigHandler is the handler for the apply-config hook.
type applyConfigHandler struct {
	context *hookstate.Context
}

func newApplyConfigHandler(context *hookstate.Context) hookstate.Handler {
	return &applyConfigHandler{context: context}
}

// Before is called by the HookManager before the apply-config hook is run.
func (h *applyConfigHandler) Before() error {
	return nil
}

// Done is called by the HookManager after the apply-config hook has exited
// successfully.
func (h *applyConfigHandler) Done() error {
	return nil
}

// Error is called by the HookManager after the apply-config hook has exited
// non-zero, and includes the error.
func (h *applyConfigHandler) Error(err error) error {
	return nil
}
