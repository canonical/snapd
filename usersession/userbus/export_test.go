// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2026 Canonical Ltd
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
 */

package userbus

import (
	"context"
	"os/exec"

	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/testutil"
)

var HelperCommand = helperCommand
var ConnectBridge = connectBridge

func MockCommandContext(f func(context.Context, string, ...string) *exec.Cmd) func() {
	return testutil.Mock(&commandContext, f)
}

func MockLookupUser(f func(string) (*user.User, error)) func() {
	return testutil.Mock(&lookupUser, f)
}

func MockGeteuid(uid int) func() {
	return testutil.Mock(&geteuid, func() int { return uid })
}

func MockInternalLibExecDir(f func() (string, error)) func() {
	return testutil.Mock(&internalLibExecDir, f)
}

func BridgeDone(c *Connection) <-chan struct{} { return c.bridge.done }
