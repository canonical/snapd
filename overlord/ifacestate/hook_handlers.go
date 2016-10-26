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
	"github.com/snapcore/snapd/overlord/state"
)

type collectAttrHandler struct {
	context *hookstate.Context
}

type confirmConnectionHandler struct{}

func (h *collectAttrHandler) Before() error {
	var id string

	h.context.Lock()
	defer h.context.Unlock()
	err := h.context.Get("connect-task", &id)
	if err != nil {
		return err
	}
	st := h.context.State()
	ts := st.Task(id)
	if ts == nil {
		panic("Failed to find connect-task")
	}

	var attrs map[string]string
	err = ts.Get("attributes", &attrs)
	if err == state.ErrNoState {
		return nil
	}

	return err
}

func (h *collectAttrHandler) Done() error {
	h.context.Lock()
	defer h.context.Unlock()

	var attrs map[string]string
	err := h.context.Get("attributes", &attrs)
	if err == state.ErrNoState {
		return nil
	}

	if err != nil {
		var id string
		err := h.context.Get("connect-task", &id)
		if err != nil {
			return err
		}
		state := h.context.State()
		ts := state.Task(id)
		if ts == nil {
			panic("Failed to find connect-task")
		}
		ts.Set("attributes", attrs)
	}
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
		return &collectAttrHandler{context: context}
	}

	prepSlotGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &collectAttrHandler{context: context}
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
