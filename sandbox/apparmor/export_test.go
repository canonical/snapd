// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package apparmor

func MockFsRootPath(path string) (restorer func()) {
	old := rootPath
	rootPath = path
	return func() {
		rootPath = old
	}
}

func MockParserSearchPath(new string) (restore func()) {
	oldAppArmorParserSearchPath := parserSearchPath
	parserSearchPath = new
	return func() {
		parserSearchPath = oldAppArmorParserSearchPath
	}
}

var (
	ProbeKernelFeatures = probeKernelFeatures
	ProbeParserFeatures = probeParserFeatures

	RequiredKernelFeatures  = requiredKernelFeatures
	RequiredParserFeatures  = requiredParserFeatures
	PreferredKernelFeatures = preferredKernelFeatures
	PreferredParserFeatures = preferredParserFeatures
)

func FreshAppArmorAssessment() {
	appArmorAssessment = &appArmorAssess{appArmorProber: &appArmorProbe{}}
}
