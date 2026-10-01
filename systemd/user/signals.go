// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

package user

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

// SignalHandler preserves wire ordering with bounded storage. Unlike godbus's
// default handler it does not spawn goroutines when a receiver falls behind.
// Overflow invalidates the client rather than silently losing job completions.
// Install it with dbus.WithSignalHandler when creating the connection.
type SignalHandler struct {
	mu       sync.Mutex
	channels map[chan<- *dbus.Signal]bool
	overflow chan struct{}
	failed   bool
	closed   bool
}

// NewSignalHandler returns a bounded handler for a private manager connection.
func NewSignalHandler() *SignalHandler {
	return &SignalHandler{channels: make(map[chan<- *dbus.Signal]bool), overflow: make(chan struct{})}
}

func (h *SignalHandler) DeliverSignal(_, _ string, signal *dbus.Signal) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.failed {
		return
	}
	for ch := range h.channels {
		select {
		case ch <- signal:
		default:
			h.failed = true
			close(h.overflow)
			return
		}
	}
}

func (h *SignalHandler) AddSignal(ch chan<- *dbus.Signal) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(ch)
		return
	}
	h.channels[ch] = true
}

func (h *SignalHandler) RemoveSignal(ch chan<- *dbus.Signal) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.channels, ch)
}

func (h *SignalHandler) Terminate() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.channels {
		close(ch)
	}
	h.channels = nil
}
