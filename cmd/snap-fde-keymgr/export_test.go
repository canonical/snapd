// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
package main

import (
	"io"

	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

var Run = run

func MockChangeEncryptionKey(f func(device string, stage, transition bool, key keys.EncryptionKey) error) (restore func()) {
	restore = testutil.Backup(&fdeKeymgrChangeEncryptionKey)
	fdeKeymgrChangeEncryptionKey = f
	return restore
}

func MockOsStdin(r io.Reader) (restore func()) {
	restore = testutil.Backup(&osStdin)
	osStdin = r
	return restore
}
