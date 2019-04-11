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

	"github.com/jessevdk/go-flags"
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
			return &cmdGetBaseDeclaration{}
		}, nil, nil)
	cmd.hidden = true

	cmd = addDebugCommand("base-declaration",
		"(internal) obtain the base declaration for all interfaces",
		"(internal) obtain the base declaration for all interfaces",
		func() flags.Commander {
			return &cmdGetBaseDeclaration{get: true}
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
	var err error
	if x.get {
		err = x.client.DebugGet("base-declaration", &resp, nil)
	} else {
		err = x.client.Debug("get-base-declaration", nil, &resp)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "%s\n", resp.BaseDeclaration)
	return nil
}
