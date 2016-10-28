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
	"fmt"
	"regexp"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/logger"
)

type collectAttrHandler struct {
	context *hookstate.Context
}

type confirmConnectionHandler struct {
	context *hookstate.Context
}

func copyAttributesFromConnectTask(context *hookstate.Context) error {
	var id string
	err := context.Get("connect-task", &id)
	if err != nil {
		return err
	}
	st := context.State()
	ts := st.Task(id)
	if ts == nil {
		return fmt.Errorf("Failed to find connect-task")
	}

	var attrs map[string]string
	err = ts.Get("attributes", &attrs)
	if err == state.ErrNoState {
		return nil
	}

	if err == nil {
		context.Set("attributes", attrs)
	}

	return err
}

func copyAttributesToConnectTask(context *hookstate.Context) error {
	var attrs map[string]string
	err := context.Get("attributes", &attrs)
	if err == state.ErrNoState {
		return nil
	}

	if err != nil {
		return err
	}

	var id string
	err = context.Get("connect-task", &id)
	if err != nil {
		return err
	}
	state := context.State()
	ts := state.Task(id)
	if ts == nil {
		return fmt.Errorf("Failed to find connect-task")
	}
	ts.Set("attributes", attrs)
	return nil
}

func (h *collectAttrHandler) Before() error {
	logger.Debugf("collect attr handler !!!")
	h.context.Lock()
	defer h.context.Unlock()
	return copyAttributesFromConnectTask(h.context)
}

func (h *collectAttrHandler) Done() error {
	h.context.Lock()
	defer h.context.Unlock()
	return copyAttributesToConnectTask(h.context)
}

func (h *collectAttrHandler) Error(err error) error {
	return nil
}

func (h *confirmConnectionHandler) Before() error {
	h.context.Lock()
	defer h.context.Unlock()
	return copyAttributesFromConnectTask(h.context)
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
		return &confirmConnectionHandler{context: context}
	}

	confirmSlotGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &confirmConnectionHandler{context: context}
	}

	hookMgr.Register(regexp.MustCompile("^prepare-plug-[a-zA-Z0-9_\\-]+$"), prepPlugGenerator)
	hookMgr.Register(regexp.MustCompile("^prepare-slot-[a-zA-Z0-9_\\-]+$"), prepSlotGenerator)
	hookMgr.Register(regexp.MustCompile("^confirm-plug-[a-zA-Z0-9_\\-]+$"), confirmPlugGenerator)
	hookMgr.Register(regexp.MustCompile("^confirm-slot-[a-zA-Z0-9_\\-]+$"), confirmSlotGenerator)
}
