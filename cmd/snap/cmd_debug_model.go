// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
)

type cmdGetModel struct {
	clientMixin
}

func init() {
	cmd := addDebugCommand("model",
		"(internal) obtain the active model assertion",
		"(internal) obtain the active model assertion",
		func() flags.Commander {
			return &cmdGetModel{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdGetModel) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	var resp struct {
		Model string `json:"model"`
	}
	mylog.Check(x.client.DebugGet("model", &resp, nil))

	fmt.Fprintf(Stdout, "%s\n", resp.Model)
	return nil
}
