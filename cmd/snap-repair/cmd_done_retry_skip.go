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
	"os"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
)

func init() {
	cmd := mylog.Check2(parser.AddCommand("done", "Signal repair is done", "", &cmdDone{}))

	cmd.Hidden = true

	cmd = mylog.Check2(parser.AddCommand("skip", "Signal repair should be skipped", "", &cmdSkip{}))

	cmd.Hidden = true

	cmd = mylog.Check2(parser.AddCommand("retry", "Signal repair must be retried next time", "", &cmdRetry{}))

	cmd.Hidden = true
}

func writeToStatusFD(msg string) error {
	statusFdStr := os.Getenv("SNAP_REPAIR_STATUS_FD")
	if statusFdStr == "" {
		return fmt.Errorf("cannot find SNAP_REPAIR_STATUS_FD environment")
	}
	fd := mylog.Check2(strconv.Atoi(statusFdStr))

	f := os.NewFile(uintptr(fd), "<snap-repair-status-fd>")
	defer f.Close()
	mylog.Check2(f.Write([]byte(msg + "\n")))

	return nil
}

type cmdDone struct{}

func (c *cmdDone) Execute(args []string) error {
	return writeToStatusFD("done")
}

type cmdSkip struct{}

func (c *cmdSkip) Execute([]string) error {
	return writeToStatusFD("skip")
}

type cmdRetry struct{}

func (c *cmdRetry) Execute([]string) error {
	return writeToStatusFD("retry")
}
