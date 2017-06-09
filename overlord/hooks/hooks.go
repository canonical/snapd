// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package hooks

import (
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	"regexp"
	"time"
)

// HookManager is responsible for the maintenance of hooks in the system state.
type HookManager interface {
	Register(pattern *regexp.Regexp, generator HandlerGenerator)
	Ensure() error
	Wait()
	Stop()
	Context(contextID string) (Context, error)
}

// HookSetup is a reference to a hook within a specific snap.
type HookSetup struct {
	Snap     string        `json:"snap"`
	Revision snap.Revision `json:"revision"`
	Hook     string        `json:"hook"`
	Optional bool          `json:"optional,omitempty"`

	Timeout     time.Duration `json:"timeout,omitempty"`
	IgnoreError bool          `json:"ignore-error,omitempty"`
	TrackError  bool          `json:"track-error,omitempty"`
}

// Context represents the context under which a given hook is running.
type Context interface {
	SnapName() string
	SnapRevision() snap.Revision
	HookName() string

	Lock()
	Unlock()

	Set(key string, value interface{})
	Get(key string, value interface{}) error
	OnDone(f func() error)
	Cache(key, value interface{})
	Cached(key interface{}) interface{}
	Done() error
	State() *state.State
}

// Handler is the interface a client must satify to handle hooks.
type Handler interface {
	// Before is called right before the hook is to be run.
	Before() error

	// Done is called right after the hook has finished successfully.
	Done() error

	// Error is called if the hook encounters an error while running.
	Error(err error) error
}

// HandlerGenerator is the function signature required to register for hooks.
type HandlerGenerator func(Context) Handler
