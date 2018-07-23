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

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// configureHandler is the handler for the configure hook.
type configureHandler struct {
	context *hookstate.Context
}

// cachedTransaction is the index into the context cache where the initialized
// transaction is stored.
type cachedTransaction struct{}

// ContextTransaction retrieves the transaction cached within the context (and
// creates one if it hasn't already been cached).
func ContextTransaction(context *hookstate.Context) *config.Transaction {
	// Check for one already cached
	tr, ok := context.Cached(cachedTransaction{}).(*config.Transaction)
	if ok {
		return tr
	}

	// It wasn't already cached, so create and cache a new one
	tr = config.NewTransaction(context.State())

	context.OnDone(func() error {
		tr.Commit()
		if context.SnapName() == "core" {
			// make sure the Ensure logic can process
			// system configuration changes as soon as possible
			context.State().EnsureBefore(0)
		}
		return nil
	})

	context.Cache(cachedTransaction{}, tr)
	return tr
}

func newConfigureHandler(context *hookstate.Context) hookstate.Handler {
	return &configureHandler{context: context}
}

// Before is called by the HookManager before the configure hook is run.
func (h *configureHandler) Before() error {
	h.context.Lock()
	defer h.context.Unlock()

	tr := ContextTransaction(h.context)

	// Initialize the transaction if there's a patch provided in the
	// context or useDefaults is set in which case gadget defaults are used.

	var patch map[string]interface{}
	var useDefaults bool
	if err := h.context.Get("use-defaults", &useDefaults); err != nil && err != state.ErrNoState {
		return err
	}

	snapName := h.context.SnapName()
	st := h.context.State()
	if useDefaults {
		var err error
		patch, err = snapstate.ConfigDefaults(st, snapName)
		if err != nil && err != state.ErrNoState {
			return err
		}
		if len(patch) != 0 {
			// TODO: helper on context?
			info, err := snapstate.CurrentInfo(st, snapName)
			if err != nil {
				return err
			}
			if info.Hooks["configure"] == nil {
				return fmt.Errorf("cannot apply gadget config defaults for snap %q, no configure hook", snapName)
			}
		}
	} else {
		if err := h.context.Get("patch", &patch); err != nil && err != state.ErrNoState {
			return err
		}
	}

	for key, value := range patch {
		if err := tr.Set(snapName, key, value); err != nil {
			return err
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
