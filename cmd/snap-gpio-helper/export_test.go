// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"context"

	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	Run = run
)

func MockGpioExportGadgetChardevChip(f func(ctx context.Context, chipLabels []string, lines strutil.Range, gadgetName string, slotName string) error) (restore func()) {
	return testutil.Mock(&gpioExportGadgetChardevChip, f)
}

func MockGpioUnxportGadgetChardevChip(f func(gadgetName string, slotName string) error) (restore func()) {
	return testutil.Mock(&gpioUnexportGadgetChardevChip, f)
}

func MockGpioEnsureAggregatorDriver(f func() error) (restore func()) {
	return testutil.Mock(&gpioEnsureAggregatorDriver, f)
}
