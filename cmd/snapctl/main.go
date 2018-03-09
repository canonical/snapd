// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/xdgopenproxy"
)

var clientConfig = client.Config{
	// snapctl should not try to read $HOME/.snap/auth.json, this will
	// result in apparmor denials and configure task failures
	// (LP: #1660941)
	DisableAuth: true,

	// we need the less privileged snap socket in snapctl
	Socket: dirs.SnapSocket,
}

func main() {
	// check for internal commands
	if len(os.Args) > 2 && os.Args[1] == "internal" {
		switch os.Args[2] {
		case "configure-core":
			fmt.Fprintf(os.Stderr, "no internal core configuration anymore")
			os.Exit(1)
		}
	}
	if len(os.Args) == 3 && os.Args[1] == "user-open" {
		if err := xdgopenproxy.Run(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "user-open error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// no internal command, route via snapd
	stdout, stderr, err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	if stdout != nil {
		os.Stdout.Write(stdout)
	}

	if stderr != nil {
		os.Stderr.Write(stderr)
	}
}

func run() (stdout, stderr []byte, err error) {
	cli := client.New(&clientConfig)

	cookie := os.Getenv("SNAP_COOKIE")
	// for compatibility, if re-exec is not enabled and facing older snapd.
	if cookie == "" {
		cookie = os.Getenv("SNAP_CONTEXT")
	}
	return cli.RunSnapctl(&client.SnapCtlOptions{
		ContextID: cookie,
		Args:      os.Args[1:],
	})
}
