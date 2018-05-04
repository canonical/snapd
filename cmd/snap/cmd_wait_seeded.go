// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"time"

	"github.com/jessevdk/go-flags"
)

type cmdWaitSeeded struct{}

func init() {
	cmd := addCommand("wait-seeded",
		"Internal",
		"The wait-seeded command waits until the machine is seeded.",
		func() flags.Commander {
			return &cmdWaitSeeded{}
		}, nil, nil)
	cmd.hidden = true
}

var waitSeededTimeout = 500 * time.Millisecond

func (x *cmdWaitSeeded) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	cli := Client()

	for {
		sysinfo, err := cli.SysInfo()
		if err != nil {
			return err
		}
		if sysinfo.Seeded {
			break
		}
		time.Sleep(waitSeededTimeout)
	}

	return nil
}
