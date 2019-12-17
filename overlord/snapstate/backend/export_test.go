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

package backend

import (
	"os/exec"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/snap"
)

var (
	AddMountUnit    = addMountUnit
	RemoveMountUnit = removeMountUnit
)

func MockUpdateFontconfigCaches(f func() error) (restore func()) {
	oldUpdateFontconfigCaches := updateFontconfigCaches
	updateFontconfigCaches = f
	return func() {
		updateFontconfigCaches = oldUpdateFontconfigCaches
	}
}

func MockCommandFromSystemSnap(f func(string, ...string) (*exec.Cmd, error)) (restore func()) {
	old := commandFromSystemSnap
	commandFromSystemSnap = f
	return func() {
		commandFromSystemSnap = old
	}
}

func MockBootParticipant(f func(s snap.PlaceInfo, t snap.Type, dev boot.Device) boot.BootParticipant) (restore func()) {
	old := bootParticipant
	bootParticipant = f
	return func() {
		bootParticipant = old
	}
}
