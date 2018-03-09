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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

var RunMain = run

var (
	CreateUserDataDirs = createUserDataDirs
	ResolveApp         = resolveApp
	IsReexeced         = isReexeced
	MaybePrintServices = maybePrintServices
	MaybePrintCommands = maybePrintCommands
	SortByPath         = sortByPath
	AdviseCommand      = adviseCommand
	Antialias          = antialias
	FormatChannel      = fmtChannel
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

func MockStoreNew(f func(*store.Config, auth.AuthContext) *store.Store) (restore func()) {
	storeNewOrig := storeNew
	storeNew = f
	return func() {
		storeNew = storeNewOrig
	}
}

func MockGetEnv(f func(name string) string) (restore func()) {
	osGetenvOrig := osGetenv
	osGetenv = f
	return func() {
		osGetenv = osGetenvOrig
	}
}

func MockMountInfoPath(newMountInfoPath string) (restore func()) {
	mountInfoPathOrig := mountInfoPath
	mountInfoPath = newMountInfoPath
	return func() {
		mountInfoPath = mountInfoPathOrig
	}
}

func MockOsReadlink(f func(string) (string, error)) (restore func()) {
	osReadlinkOrig := osReadlink
	osReadlink = f
	return func() {
		osReadlink = osReadlinkOrig
	}
}

var AutoImportCandidates = autoImportCandidates

func AliasInfoLess(snapName1, alias1, cmd1, snapName2, alias2, cmd2 string) bool {
	x := aliasInfos{
		&aliasInfo{
			Snap:    snapName1,
			Alias:   alias1,
			Command: cmd1,
		},
		&aliasInfo{
			Snap:    snapName2,
			Alias:   alias2,
			Command: cmd2,
		},
	}
	return x.Less(0, 1)
}

func AssertTypeNameCompletion(match string) []flags.Completion {
	return assertTypeName("").Complete(match)
}

func MockIsTerminal(t bool) (restore func()) {
	oldIsTerminal := isTerminal
	isTerminal = func() bool { return t }
	return func() {
		isTerminal = oldIsTerminal
	}
}

func MockTimeNow(newTimeNow func() time.Time) (restore func()) {
	oldTimeNow := timeNow
	timeNow = newTimeNow
	return func() {
		timeNow = oldTimeNow
	}
}

func MockTimeutilHuman(h func(time.Time) string) (restore func()) {
	oldH := timeutilHuman
	timeutilHuman = h
	return func() {
		timeutilHuman = oldH
	}
}

func Wait(cli *client.Client, id string) (*client.Change, error) {
	return waitMixin{}.wait(cli, id)
}
