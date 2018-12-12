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
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
)

func (b *Backend) AddMountUnit(s *snap.Info, meter progress.Meter) error {
	return b.addMountUnit(s, meter)
}

func (b *Backend) RemoveMountUnit(baseDir string, meter progress.Meter) error {
	return b.removeMountUnit(baseDir, meter)
}

func MockUpdateFontconfigCaches(f func() error) (restore func()) {
	oldUpdateFontconfigCaches := updateFontconfigCaches
	updateFontconfigCaches = f
	return func() {
		updateFontconfigCaches = oldUpdateFontconfigCaches
	}
}
