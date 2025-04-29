// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package vfs

type Event struct {
	Op    string
	Mount mount
}

func (v *VFS) SetObserver(fn func(Event)) {
	v.mu.Lock()
	v.observer = fn
	v.mu.Unlock()
}

func (v *VFS) sendEvent(op string, m *mount) {
	if fn := v.observer; fn != nil {
		fn(Event{Op: op, Mount: *m})
	}
}

func (v *VFS) SetLogger(l interface {
	Log(...any)
	Logf(string, ...any)
}) {
	v.mu.Lock()
	v.l = l
	v.mu.Unlock()
}

type logger interface {
	Log(...any)
	Logf(string, ...any)
}

type nopL struct{}

func (nopL) Log(...any)          {}
func (nopL) Logf(string, ...any) {}
