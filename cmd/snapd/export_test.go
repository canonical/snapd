// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
)

var (
	Run = run
)

func MockSelftestRun(f func() error) (restore func()) {
	oldSelftestRun := selftestRun
	selftestRun = f
	return func() {
		selftestRun = oldSelftestRun
	}
}

func MockCheckRunningConditionsRetryDelay(d time.Duration) (restore func()) {
	oldCheckRunningConditionsRetryDelay := checkRunningConditionsRetryDelay
	checkRunningConditionsRetryDelay = d
	return func() {
		checkRunningConditionsRetryDelay = oldCheckRunningConditionsRetryDelay
	}
}
