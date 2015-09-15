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
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/partition"
)

type cmdGrubMigrate struct {
}

func init() {
	_, err := parser.AddCommand("grub-migrate",
		"internal",
		"internal",
		&cmdGrubMigrate{})
	if err != nil {
		logger.Panicf("Unable to grub-migrate: %v", err)
	}
}

func (x *cmdGrubMigrate) Execute(args []string) error {
	return withMutex(x.doGrubMigrate)
}

func (x *cmdGrubMigrate) doGrubMigrate() error {
	return partition.MigrateToDynamicGrub()
}
