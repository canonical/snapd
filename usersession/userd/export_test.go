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

package userd

import (
	"github.com/godbus/dbus"
)

func MockSnapFromSender(f func(*dbus.Conn, dbus.Sender) (string, error)) func() {
	origSnapFromSender := snapFromSender
	snapFromSender = f
	return func() {
		snapFromSender = origSnapFromSender
	}
}

type FileExists fileExists

func DesktopFileIDToFilename(desktopFileExists FileExists, desktopFileID string) (string, error) {
	return desktopFileIDToFilename(fileExists(desktopFileExists), desktopFileID)
}

func ParseExecCommand(exec_command string, icon string) ([]string, error) {
	return parseExecCommand(exec_command, icon)
}

func ReadExecCommandFromDesktopFile(desktopFile string) (exec string, icon string, err error) {
	return readExecCommandFromDesktopFile(desktopFile)
}
