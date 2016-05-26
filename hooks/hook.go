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

// The hooks package contains all hook runners supported by snapd.
package hooks

// DispatchHook is the hook dispatcher; it's exported here so it can be
// overridden in tests.
var DispatchHook = doDispatchHook

// HookRef is a reference to a hook within a specific snap.
type HookRef struct {
	Snap string `json:"snap"`
	Hook string `json:"hook"`
}

// doDispatchHook is where the snap in question is found and the specific hook
// is run. TODO: hooks don't actually exist yet, so nothing to dispatch.
func doDispatchHook(hook HookRef) error {
	return nil
}
