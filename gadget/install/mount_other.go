// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !linux

/*
 * Copyright (C) 2020 Canonical Ltd
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

package install

import (
	"errors"
	"syscall"
)

var (
	sysMount   = unimplementedMount
	sysUnmount = syscall.Unmount
)

func unimplementedMount(source string, target string, fstype string, flags uintptr, data string) error {
	return errors.New("mount not implemented")
}
