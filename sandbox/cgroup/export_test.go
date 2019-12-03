// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package cgroup

import (
	. "gopkg.in/check.v1"
)

var (
	Cgroup2SuperMagic         = cgroup2SuperMagic
	ProbeCgroupVersion        = probeCgroupVersion
	ParsePid                  = parsePid
	SecurityTagFromCgroupPath = securityTagFromCgroupPath
)

func MockFsTypeForPath(mock func(string) (int64, error)) (restore func()) {
	old := fsTypeForPath
	fsTypeForPath = mock
	return func() {
		fsTypeForPath = old
	}
}

func MockFsRootPath(p string) (restore func()) {
	old := rootPath
	rootPath = p
	return func() {
		rootPath = old
	}
}

func MockFreezerCgroupDir(c *C) (restore func()) {
	old := freezerCgroupDir
	freezerCgroupDir = c.MkDir()
	return func() {
		freezerCgroupDir = old
	}
}

func FreezerCgroupDir() string {
	return freezerCgroupDir
}
