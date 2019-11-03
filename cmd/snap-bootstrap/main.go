// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
	"github.com/snapcore/snapd/osutil"
)

func run(args []string) error {
	if !osutil.GetenvBool("SNAPPY_TESTING") {
		return fmt.Errorf("cannot use outside of tests yet")
	}
	if os.Getuid() != 0 {
		return fmt.Errorf("please run as root")
	}

	var opts struct {
		WithEncryption bool `long:"with-encryption" description:"Encrypt the data partition"`

		Args struct {
			GadgetRoot string `positional-arg-name:"gadget-root"`
			Device     string `positional-arg-name:"block-device"`
		} `positional-args:"yes" required:"yes"`
	}

	if _, err := flags.ParseArgs(&opts, args[1:]); err != nil {
		os.Exit(1)
	}

	options := &bootstrap.Options{
		EncryptDataPartition: opts.WithEncryption,
	}

	return bootstrap.Run(opts.Args.GadgetRoot, opts.Args.Device, options)
}

func main() {
	err := run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
