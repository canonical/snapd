// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io"
)

var (
	OutputUI          = outputUI
	RunUI             = runUI
	Chooser           = chooser
	ExecuteMenuAction = executeMenuAction
)

func MockStdStreams(stdout, stderr io.Writer) (restore func()) {
	oldStdout, oldStderr := Stdout, Stderr
	Stdout, Stderr = stdout, stderr
	return func() {
		Stdout, Stderr = oldStdout, oldStderr
	}
}

func MockToolPath(f func() (string, error)) (restore func()) {
	oldToolPath := toolPath
	toolPath = f
	return func() {
		toolPath = oldToolPath
	}
}

func MockExecuteAction(f func(action string) error) (restore func()) {
	oldExecuteAction := f
	executeAction = f
	return func() {
		executeAction = oldExecuteAction
	}
}

func MockDefaultMarkerFile(p string) (restore func()) {
	old := defaultMarkerFile
	defaultMarkerFile = p
	return func() {
		defaultMarkerFile = old
	}
}
