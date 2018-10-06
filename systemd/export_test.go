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

package systemd

import (
	"io"
)

var (
	Jctl = jctl
)

func MockOsGetenv(f func(string) string) func() {
	oldOsGetenv := osGetenv
	osGetenv = f
	return func() { osGetenv = oldOsGetenv }
}

func MockOsutilStreamCommand(f func(string, ...string) (io.ReadCloser, error)) func() {
	old := osutilStreamCommand
	osutilStreamCommand = f
	return func() { osutilStreamCommand = old }
}

func MockJournalStdoutPath(path string) func() {
	oldPath := journalStdoutPath
	journalStdoutPath = path
	return func() {
		journalStdoutPath = oldPath
	}
}
