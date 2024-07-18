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
	"runtime"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

func serverVersion(cli *client.Client) *client.ServerVersion {
	if release.OnWSL && release.WSLVersion == 1 {
		return &client.ServerVersion{
			Version:       i18n.G("unavailable"),
			Series:        release.Series,
			OSID:          "Windows Subsystem for Linux",
			OnClassic:     true,
			KernelVersion: fmt.Sprintf("%s (%s)", osutil.KernelVersion(), runtime.GOARCH),
		}
	}
	sv, err := cli.ServerVersion()

	if err != nil {
		sv = &client.ServerVersion{
			Version:     i18n.G("unavailable"),
			Series:      "-",
			OSID:        "-",
			OSVersionID: "-",
		}
	}
	return sv
}
