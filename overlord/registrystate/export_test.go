// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2024 Canonical Ltd
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

package registrystate

import (
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
)

var (
	ReadDatabag  = readDatabag
	WriteDatabag = writeDatabag
)

func MockReadDatabag(f func(st *state.State, account, registryName string) (registry.JSONDataBag, error)) func() {
	old := readDatabag
	readDatabag = f
	return func() {
		readDatabag = old
	}
}

func MockWriteDatabag(f func(st *state.State, databag registry.JSONDataBag, account, registryName string) error) func() {
	old := writeDatabag
	writeDatabag = f
	return func() {
		writeDatabag = old
	}
}
