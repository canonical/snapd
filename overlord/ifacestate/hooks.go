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
	"fmt"
	"regexp"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

type prepareHandler struct {
	context *hookstate.Context
}

type connectHandler struct {
	context *hookstate.Context
}

func connectTask(context *hookstate.Context) (*state.Task, error) {
	var id string
	err := context.Get("connect-task", &id)
	if err != nil {
		return nil, err
	}
	state := context.State()
	ts := state.Task(id)
	if ts == nil {
		return nil, fmt.Errorf("Failed to find connect-task")
	}
	return ts, nil
}

func copyAttributesFromConnectTask(context *hookstate.Context) (err error) {
	var ts *state.Task
	if ts, err = connectTask(context); err != nil {
		return err
	}

	var attrs map[string]interface{}
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
	var attrs map[string]interface{}
	err := context.Get("attributes", &attrs)
	if err == state.ErrNoState {
		return nil
	}

	if err != nil {
		return err
	}

	var ts *state.Task
	if ts, err = connectTask(context); err != nil {
		return err
	}

	ts.Set("attributes", attrs)
	return nil
}

func (h *prepareHandler) Before() error {
	h.context.Lock()
	defer h.context.Unlock()
	return copyAttributesFromConnectTask(h.context)
}

func (h *prepareHandler) Done() error {
	h.context.Lock()
	defer h.context.Unlock()
	return copyAttributesToConnectTask(h.context)
}

func (h *prepareHandler) Error(err error) error {
	return nil
}

func (h *connectHandler) Before() error {
	h.context.Lock()
	defer h.context.Unlock()
	return copyAttributesFromConnectTask(h.context)
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
