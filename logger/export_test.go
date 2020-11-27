// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014,2015,2017 Canonical Ltd
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

package logger

func GetLogger() Logger {
	lock.Lock()
	defer lock.Unlock()

	return logger
}

func GetLoggerFlags() int {
	log, ok := GetLogger().(*Log)
	if !ok {
		return -1
	}

	return log.log.Flags()
}

func ProcCmdlineMustMock(new bool) (restore func()) {
	old := procCmdlineUseDefaultMockInTests
	procCmdlineUseDefaultMockInTests = new
	return func() {
		procCmdlineUseDefaultMockInTests = old
	}
}
