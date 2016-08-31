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
	context     *hookstate.Context
	transaction *Transaction
}

// Before is called by the HookManager before the apply-config hook is run.
func (h *applyConfigHandler) Before() error {
	return nil
}

// Done is called by the HookManager after the apply-config hook has exited
// successfully.
func (h *applyConfigHandler) Done() error {
	// Save any configurations changes the hook may have made
	h.transaction.Commit()
	return nil
}

// Error is called by the HookManager after the apply-config hook has exited
// non-zero, and includes the error.
func (h *applyConfigHandler) Error(err error) error {
	// Note that in this case, Done() is not called, which means any
	// configuration changes made by the hook are dropped.
	return nil
}

// SetConf is called by `snapctl set` to associate the value with the key in
// the snap's configuration.
func (h *applyConfigHandler) SetConf(key string, value interface{}) {
	// Set this in the transaction, but don't commit it (wait until Done())
	h.transaction.Set(h.context.SnapName(), key, value)
}
