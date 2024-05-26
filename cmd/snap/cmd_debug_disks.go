// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"encoding/json"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/gadget"
)

type cmdDiskMapping struct {
	clientMixin
}

func init() {
	cmd := addDebugCommand("disks",
		"(internal) obtain all on-disk volume information as JSON",
		"(internal) obtain all on-disk volume information as JSON",
		func() flags.Commander {
			return &cmdDiskMapping{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdDiskMapping) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	resp := []gadget.OnDiskVolume{}
	mylog.Check(x.client.DebugGet("disks", &resp, nil))

	b := mylog.Check2(json.Marshal(resp))

	fmt.Fprintf(Stdout, "%s\n", string(b))
	return nil
}
