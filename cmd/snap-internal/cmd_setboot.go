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

package main

import (
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/partition"
)

type cmdSetBoot struct {
	Positional struct {
		Name  string `positional-arg-name:"<name>"`
		Value string `positional-arg-name:"<value>"`
	} `positional-args:"yes" required:"yes"`

	RootDir string `long:"rootdir"`
}

func (x *cmdSetBoot) Execute([]string) error {
	if x.RootDir != "" {
		dirs.SetRootDir(x.RootDir)
	}

	bootloader, err := partition.FindBootloader()
	if err != nil {
		return err
	}

	return bootloader.SetBootVar(x.Positional.Name, x.Positional.Value)
}
