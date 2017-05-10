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

package snap

var (
	ImplicitSlotsForTests        = implicitSlots
	ImplicitClassicSlotsForTests = implicitClassicSlots
	NewHookType                  = newHookType
)

func MockSupportedHookTypes(hookTypes []*HookType) (restore func()) {
	old := supportedHooks
	supportedHooks = hookTypes
	return func() { supportedHooks = old }
}

func (info *Info) RenamePlug(oldName, newName string) {
	info.renamePlug(oldName, newName)
}
