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
)

var clientConfig client.Config

func main() {
	stdout, stderr, err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	if stdout != "" {
		fmt.Fprintf(os.Stdout, stdout)
	}

	if stderr != "" {
		fmt.Fprintf(os.Stderr, stderr)
	}
}

func run() (stdout string, stderr string, err error) {
	cli := client.New(&clientConfig)

	context := os.Getenv("SNAP_CONTEXT")
	if context == "" {
		return "", "", fmt.Errorf("snaptool requires SNAP_CONTEXT environment variable")
	}

	return cli.RunSnaptool(context, os.Args[1:])
}
