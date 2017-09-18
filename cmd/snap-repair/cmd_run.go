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
	"net/url"

	"github.com/snapcore/snapd/osutil"
)

func init() {
	const (
		short = "Fetch and run repair assertions as necessary for the device"
		long  = ""
	)

	if _, err := parser.AddCommand("run", short, long, &cmdRun{}); err != nil {
		panic(err)
	}

}

type cmdRun struct{}

var baseURL *url.URL

func init() {
	var baseurl string
	if osutil.GetenvBool("SNAPPY_USE_STAGING_STORE") {
		baseurl = "https://api.staging.snapcraft.io/v2/"
	} else {
		baseurl = "https://api.snapcraft.io/v2/"
	}

	var err error
	baseURL, err = url.Parse(baseurl)
	if err != nil {
		panic(fmt.Sprintf("cannot setup base url: %v", err))
	}
}

func (c *cmdRun) Execute(args []string) error {
	// TODO: operate a run lock

	run := NewRunner()
	run.BaseURL = baseURL
	err := run.LoadState()

	if err != nil {
		return err
	}

	for {
		repair, err := run.Next("canonical")
		if err == ErrRepairNotFound {
			// no more repairs
			break
		}
		if err != nil {
			return err
		}

		if err := repair.Run(); err != nil {
			return err
		}
	}
	return nil
}
