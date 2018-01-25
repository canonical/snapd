// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	. "gopkg.in/check.v1"
)

var (
	GetDeviceCgroupFn = getDeviceCgroupFn
	GetAcl            = getAcl
	DoRun             = run
)

func MockDeviceCgroupDir(c *C) (restore func()) {
	old := deviceCgroupDir
	deviceCgroupDir = c.MkDir()
	return func() {
		deviceCgroupDir = old
	}
}

func DeviceCgroupDir() string {
	return deviceCgroupDir
}

func InitLoggerFail() error {
	return fmt.Errorf("mock failure")
}

func MockInitLogger(f func() error) func() {
	old := initLogger
	initLogger = f
	return func() {
		initLogger = old
	}
}
