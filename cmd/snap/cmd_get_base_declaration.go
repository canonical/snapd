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
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

type cmdGetBaseDeclaration struct {
	get bool
	clientMixin
}

func init() {
	cmd := addDebugCommand("get-base-declaration",
		"(internal) obtain the base declaration for all interfaces (deprecated)",
		"(internal) obtain the base declaration for all interfaces (deprecated)",
		func() flags.Commander {
			return &cmdGetBaseDeclaration{get: true}
		}, nil, nil)
	cmd.hidden = true

	cmd = addDebugCommand("base-declaration",
		"(internal) obtain the base declaration for all interfaces",
		"(internal) obtain the base declaration for all interfaces",
		func() flags.Commander {
			return &cmdGetBaseDeclaration{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdGetBaseDeclaration) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	var resp struct {
		BaseDeclaration string `json:"base-declaration"`
	}

	mylog.Check(x.client.DebugGet("base-declaration", &resp, nil))

	fmt.Fprintf(Stdout, "%s\n", resp.BaseDeclaration)
	if x.get {
		fmt.Fprintf(Stderr, i18n.G("'snap debug get-base-declaration' is deprecated; use 'snap debug base-declaration'."))
	}
	return nil
}
