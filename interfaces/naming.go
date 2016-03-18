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

package interfaces

import (
	"fmt"
)

// WrapperNameForApp returns the name of the wrapper for a given application.
//
// A wrapper is a generated helper executable that assists in setting up
// environment for running a particular application.
//
// In general, the wrapper has the form: "$snap.$app". When both snap name and
// app name are the same then the tag is simplified to just "$snap".
func WrapperNameForApp(snapName, appName string) string {
	if appName == snapName {
		return snapName
	}
	return fmt.Sprintf("%s.%s", snapName, appName)
}

// SecurityTagForApp returns the unified tag used for all security systems.
//
// In general, the tag has the form: "$snap.$app.snap". When both snap name and
// app name are the same then the tag is simplified to just "$snap.snap".
func SecurityTagForApp(snapName, appName string) string {
	return fmt.Sprintf("%s.snap", WrapperNameForApp(snapName, appName))
}
