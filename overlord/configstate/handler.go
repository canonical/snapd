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

// configureHandler is the handler for the configure hook.
type configureHandler struct {
	context *hookstate.Context
}

// cachedTransaction is the index into the context cache where the initialized
// transaction is stored.
type cachedTransaction struct{}

// ContextTransaction retrieves the transaction cached within the context (and
// creates one if it hasn't already been cached).
func ContextTransaction(context *hookstate.Context) *Transaction {
	// Check for one already cached
	transaction, ok := context.Cached(cachedTransaction{}).(*Transaction)
	if ok {
		return transaction
	}

	// It wasn't already cached, so create and cache a new one
	transaction = NewTransaction(context.State())

	context.OnDone(func() error {
		transaction.Commit()
		return nil
	})

	context.Cache(cachedTransaction{}, transaction)
	return transaction
}

func NewConfigureHandler(context *hookstate.Context) hookstate.Handler {
	return &configureHandler{context: context}
}

// Before is called by the HookManager before the configure hook is run.
func (h *configureHandler) Before() error {
	h.context.Lock()
	defer h.context.Unlock()

	transaction := ContextTransaction(h.context)

	// Initialize the transaction if there's a patch provided in the
	// context.
	var patch map[string]interface{}
	if err := h.context.Get("patch", &patch); err == nil {
		for key, value := range patch {
			transaction.Set(h.context.SnapName(), key, value)
		}
	}

	return nil
}

// Done is called by the HookManager after the configure hook has exited
// successfully.
func (h *configureHandler) Done() error {
	return nil
}

// Error is called by the HookManager after the configure hook has exited
// non-zero, and includes the error.
func (h *configureHandler) Error(err error) error {
	return nil
}
