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
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/snapcore/snapd/tests/lib/fakestore/refresh"
)

type cmdNewSnapRevision struct {
	Positional struct {
		Snap string
	} `positional-args:"yes"`

	TopDir          string `long:"dir" description:"Directory to be used by the store to keep and serve snaps, <dir>/asserts is used for assertions"`
	SnapRevJsonPath string `long:"snap-rev-json" description:"Path to a json encoded snap revision"`
}

func (x *cmdNewSnapRevision) Execute(args []string) error {
	headers := map[string]interface{}{}
	if x.SnapRevJsonPath != "" {
		content, err := ioutil.ReadFile(x.SnapRevJsonPath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(content, &headers); err != nil {
			return err
		}
	}

	p, err := refresh.NewSnapRevision(x.TopDir, x.Positional.Snap, headers)
	if err != nil {
		return err
	}
	fmt.Println(p)
	return nil
}

var shortNewSnapRevisionHelp = "Make new snap revision"

func init() {
	parser.AddCommand("new-snap-revision", shortNewSnapRevisionHelp, "", &cmdNewSnapRevision{})
}
