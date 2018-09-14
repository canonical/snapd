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
	"fmt"

	"github.com/jessevdk/go-flags"
)

type cmdCanManageRefreshes struct {
	clientMixin
}

func init() {
	cmd := addDebugCommand("can-manage-refreshes",
		"(internal) return if refresh.schedule=managed can be used",
		"(internal) return if refresh.schedule=managed can be used",
		func() flags.Commander {
			return &cmdCanManageRefreshes{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdCanManageRefreshes) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	var resp bool
	if err := x.client.Debug("can-manage-refreshes", nil, &resp); err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "%v\n", resp)
	return nil
}
