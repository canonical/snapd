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

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
)

var bootstrapRun = bootstrap.Run

func init() {
	const (
		short = "Create missing partitions for the device"
		long  = ""
	)

	if _, err := parser.AddCommand("create-partitions", short, long, &cmdCreatePartitions{}); err != nil {
		panic(err)
	}
}

type cmdCreatePartitions struct {
	WithEncryption bool   `long:"with-encryption" description:"Encrypt the data partition"`
	KeyFile        string `long:"key-file" value-name:"name" description:"Where the key file will be stored"`

	Positional struct {
		GadgetRoot string `positional-arg-name:"<gadget-root>"`
		Device     string `positional-arg-name:"<device>"`
	} `positional-args:"yes"`
}

func (c *cmdCreatePartitions) Execute(args []string) error {
	if c.WithEncryption && c.KeyFile == "" {
		return fmt.Errorf("if encrypting, the output key file must be specified")
	}

	options := bootstrap.Options{
		EncryptDataPartition: c.WithEncryption,
		KeyFile:              c.KeyFile,
	}

	return bootstrapRun(c.Positional.GadgetRoot, c.Positional.Device, options)
}
