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
	"time"

	"github.com/snapcore/snapd/testutil"
	"golang.org/x/sys/unix"
)

var Run = run

func MockGetGpioInfo(f func(path string) (GPIOChardev, error)) (restore func()) {
	return testutil.Mock(&getChipInfo, f)
}

func MockUnixStat(f func(path string, stat *unix.Stat_t) (err error)) (restore func()) {
	return testutil.Mock(&unixStat, f)
}

func MockUnixMknod(f func(path string, mode uint32, dev int) (err error)) (restore func()) {
	return testutil.Mock(&unixMknod, f)
}

func MockAggregatorCreationTimeout(t time.Duration) (restore func()) {
	return testutil.Mock(&aggregatorCreationTimeout, t)
}
