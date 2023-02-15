// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/snap"
)

func init() {
	const (
		short = "Measure epoch"
		long  = "Measure epoch"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("measure-epoch", short, long, &cmdMeasureEpoch{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdMeasureEpoch struct{}

func (c *cmdMeasureEpoch) Execute([]string) error {
	return measureEpoch()
}

var (
	secbootMeasureSnapSystemEpochWhenPossible func() error
)

func measureEpoch() error {
	return secbootMeasureSnapSystemEpochWhenPossible()
}
