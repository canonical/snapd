// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/boot"
	"github.com/snapcore/snapd/partition"
)

type cmdBooted struct{}

func init() {
	cmd := addCommand("booted",
		"internal",
		"internal",
		func() flags.Commander {
			return &cmdBooted{}
		})
	cmd.hidden = true
}

func (x *cmdBooted) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("can not mark boot successful: %s", err)
	}

	if err := partition.MarkBootSuccessful(bootloader); err != nil {
		return err
	}

	ovld, err := overlord.New()
	if err != nil {
		return err
	}
	return boot.UpdateRevisions(ovld)
}
