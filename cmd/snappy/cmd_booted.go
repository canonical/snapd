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

	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdBooted struct {
}

func init() {
	_, err := parser.AddCommand("booted",
		"internal",
		"internal",
		&cmdBooted{})
	if err != nil {
		logger.Panicf("Unable to booted: %v", err)
	}
}

func (x *cmdBooted) Execute(args []string) error {
	return withMutexAndRetry(x.doBooted)
}

func (x *cmdBooted) doBooted() error {
	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("can not mark boot successful: %s", err)
	}

	if err := partition.MarkBootSuccessful(bootloader); err != nil {
		return err
	}

	return snappy.SyncBoot()
}
