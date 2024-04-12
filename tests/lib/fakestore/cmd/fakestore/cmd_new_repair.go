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
	"os"

	"github.com/snapcore/snapd/tests/lib/fakestore/refresh"
)

type cmdNewRepair struct {
	Positional struct {
		Script string
	} `positional-args:"yes"`

	TopDir     string `long:"dir" description:"Directory to be used by the store to keep and serve snaps, <dir>/asserts is used for assertions"`
	RepairJSON string `long:"repair-json" description:"Path to JSON encoded repair headers"`
}

func (x *cmdNewRepair) Execute(args []string) error {
	headers := map[string]interface{}{}
	if x.RepairJSON != "" {
		content, err := os.ReadFile(x.RepairJSON)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(content, &headers); err != nil {
			return err
		}
	}

	if x.Positional.Script == "" {
		return fmt.Errorf("script argument must be specified")
	}

	p, err := refresh.NewRepair(x.TopDir, x.Positional.Script, headers)
	if err != nil {
		return err
	}
	fmt.Println(p)
	return nil
}

var shortNewRepairHelp = "Make new repair"

func init() {
	parser.AddCommand("new-repair", shortNewRepairHelp, "", &cmdNewRepair{})
}
