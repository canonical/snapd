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

package snapenv

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/logger"
)

func fillSnapEnvVars(desc interface{}, vars []string) []string {
	for i, v := range vars {
		var templateOut bytes.Buffer
		t := template.Must(template.New("wrapper").Parse(v))
		if err := t.Execute(&templateOut, desc); err != nil {
			// this can never happen, except we forget a variable
			logger.Panicf("Unable to execute template: %v", err)
		}
		vars[i] = templateOut.String()
	}
	return vars
}

// GetBasicSnapEnvVars returns the app-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetBasicSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP={{.App.Snap.MountDir}}",
		"SNAP_DATA={{.App.Snap.DataDir}}",
		"SNAP_NAME={{.App.Snap.Name}}",
		"SNAP_VERSION={{.App.Snap.Version}}",
		"SNAP_REVISION={{.App.Snap.Revision}}",
		fmt.Sprintf("SNAP_ARCH=%s", arch.UbuntuArchitecture()),
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
	})
}

// GetUserSnapEnvVars returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetUserSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP_USER_DATA={{.Home}}{{.App.Snap.MountDir}}",
	})
}
