// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package sysparams

import (
	"os"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

func MockOsutilAtomicWriteFile(f func(filename string, data []byte, perm os.FileMode, flags osutil.AtomicWriteFlags) error) func() {
	r := testutil.Backup(&osutilAtomicWriteFile)
	osutilAtomicWriteFile = f
	return r
}
