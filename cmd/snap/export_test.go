// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os/user"
	"time"
)

var RunMain = run

var (
	SnapExecEnv        = snapExecEnv
	CreateUserDataDirs = createUserDataDirs
	SnapRunApp         = snapRunApp
	SnapRunHook        = snapRunHook
	Wait               = wait
)

func MockPollTime(d time.Duration) (restore func()) {
	d0 := pollTime
	pollTime = d
	return func() {
		pollTime = d0
	}
}

func MockMaxGoneTime(d time.Duration) (restore func()) {
	d0 := maxGoneTime
	maxGoneTime = d
	return func() {
		maxGoneTime = d0
	}
}

func MockSyscallExec(f func(string, []string, []string) error) (restore func()) {
	syscallExecOrig := syscallExec
	syscallExec = f
	return func() {
		syscallExec = syscallExecOrig
	}
}

func MockUserCurrent(f func() (*user.User, error)) (restore func()) {
	userCurrentOrig := userCurrent
	userCurrent = f
	return func() {
		userCurrent = userCurrentOrig
	}
}
