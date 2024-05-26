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

type cmdGadgetDiskMapping struct {
	clientMixin
}

func init() {
	cmd := addDebugCommand("gadget-disk-mapping",
		"(internal) obtain the gadget disk mapping",
		"(internal) obtain the gadget disk mapping",
		func() flags.Commander {
			return &cmdGadgetDiskMapping{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdGadgetDiskMapping) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	resp := map[string]gadget.DiskVolumeDeviceTraits{}
	mylog.Check(x.client.DebugGet("gadget-disk-mapping", &resp, nil))

	b := mylog.Check2(json.Marshal(resp))

	fmt.Fprintf(Stdout, "%s\n", string(b))
	return nil
}
