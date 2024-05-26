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
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/logger"
)

var opts struct {
	FromSnapConfine bool `long:"from-snap-confine"`
	UserMounts      bool `long:"user-mounts"`
	UserID          int  `short:"u"`
	Positionals     struct {
		SnapName string `positional-arg-name:"SNAP_NAME" required:"yes"`
	} `positional-args:"true"`
}

// IMPORTANT: all the code in main() until bootstrap is finished may be run
// with elevated privileges when invoking snap-update-ns from the setuid
// snap-confine.

func main() {
	logger.SimpleSetup(nil)
	mylog.Check(run())

	// END IMPORTANT
}

func parseArgs(args []string) error {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	_ := mylog.Check2(parser.ParseArgs(args))
	return err
}

// IMPORTANT: all the code in run() until BootStrapError() is finished may
// be run with elevated privileges when invoking snap-update-ns from
// the setuid snap-confine.

func run() error {
	mylog.Check(
		// There is some C code that runs before main() is started.
		// That code always runs and sets an error condition if it fails.
		// Here we just check for the error.
		BootstrapError())
	mylog.Check(
		// If there is no mount namespace to transition to let's just quit
		// instantly without any errors as there is nothing to do anymore.

		// END IMPORTANT

		parseArgs(os.Args[1:]))

	// Explicitly set the umask to 0 to prevent permission bits
	// being masked out when creating files and directories.
	//
	// While snap-confine already does this for us, we inherit
	// snapd's umask when it invokes us.
	syscall.Umask(0)

	var upCtx MountProfileUpdateContext
	if opts.UserMounts {
		userUpCtx := mylog.Check2(NewUserProfileUpdateContext(opts.Positionals.SnapName, opts.FromSnapConfine, os.Getuid()))

		upCtx = userUpCtx
	} else {
		upCtx = NewSystemProfileUpdateContext(opts.Positionals.SnapName, opts.FromSnapConfine)
	}
	return executeMountProfileUpdate(upCtx)
}
