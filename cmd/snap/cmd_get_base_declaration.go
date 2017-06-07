// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/jessevdk/go-flags"
)

type cmdGetBaseDeclaration struct{}

func init() {
	addDebugCommand("get-base-declaration",
		"(internal) obtain the base declaration for all interfaces",
		"(internal) obtain the base declaration for all interfaces",
		func() flags.Commander {
			return &cmdGetBaseDeclaration{}
		})
}

func (x *cmdGetBaseDeclaration) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	var decl []byte
	if err := Client().Debug("get-base-declaration", nil, &decl); err != nil {
		return err
	}
	if _, err := Stdout.Write(decl); err != nil {
		return err
	}
	return nil
}
