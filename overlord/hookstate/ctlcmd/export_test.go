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

package ctlcmd

import (
	"fmt"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var AttributesTask = attributesTask

func MockServicestateControlFunc(f func(*state.State, []*snap.AppInfo, *servicestate.Instruction, *hookstate.Context) ([]*state.TaskSet, error)) (restore func()) {
	old := servicestateControl
	servicestateControl = f
	return func() { servicestateControl = old }
}

func AddMockCommand(name string) *MockCommand {
	mockCommand := NewMockCommand()
	addCommand(name, "", "", func() command { return mockCommand })
	return mockCommand
}

func RemoveCommand(name string) {
	delete(commands, name)
}

type MockCommand struct {
	baseCommand

	ExecuteError bool
	FakeStdout   string
	FakeStderr   string

	Args []string
}

func NewMockCommand() *MockCommand {
	return &MockCommand{
		ExecuteError: false,
	}
}

func (c *MockCommand) Execute(args []string) error {
	c.Args = args

	if c.FakeStdout != "" {
		c.printf(c.FakeStdout)
	}

	if c.FakeStderr != "" {
		c.errorf(c.FakeStderr)
	}

	if c.ExecuteError {
		return fmt.Errorf("failed at user request")
	}

	return nil
}
