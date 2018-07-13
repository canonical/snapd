// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package hotplug

import (
	"gopkg.in/check.v1"

	"io/ioutil"
	"path/filepath"
)

func MockUdevadmbin(c *check.C, script []byte) (restore func(), err error) {
	old := udevadmBin
	restore = func() {
		udevadmBin = old
	}

	fn := filepath.Join(c.MkDir(), "udevadmmock")
	err = ioutil.WriteFile(fn, script, 0755)
	udevadmBin = fn

	return restore, err
}
