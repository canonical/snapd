// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2026 Canonical Ltd
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
 */

package snapd_userbus

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/usersession/userbus"
)

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected a numeric uid")
	}
	uid, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil {
		return fmt.Errorf("cannot parse uid: %w", err)
	}
	return userbus.RunBridge(context.Background(), uint32(uid), os.NewFile(3, "userbus"))
}

// Main runs the internal user-bus bridge with the socket inherited on FD 3.
func Main() {
	snapdtool.MaybeCompleteFIPSSetup()
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
