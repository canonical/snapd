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
	"strings"
	"text/template"

	"github.com/ubuntu-core/snappy/logger"
)

// MakeMapFromEnvList takes a string list of the form "key=value"
// and returns a map[string]string from that list
// This is useful for os.Environ() manipulation
func MakeMapFromEnvList(env []string) map[string]string {
	envMap := map[string]string{}
	for _, l := range env {
		split := strings.SplitN(l, "=", 2)
		if len(split) != 2 {
			return nil
		}
		envMap[split[0]] = split[1]
	}
	return envMap
}

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
		"SNAP={{.SnapPath}}",
		"SNAP_DATA=/var{{.SnapPath}}",
		"SNAP_NAME={{.SnapName}}",
		"SNAP_VERSION={{.Version}}",
		"SNAP_REVISION={{.Revision}}",
		"SNAP_ARCH={{.SnapArch}}",
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
	})
}

// GetUserSnapEnvVars returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetUserSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP_USER_DATA={{.Home}}{{.SnapPath}}",
	})
}

// GetDeprecatedBasicSnapEnvVars returns the app-level deprecated environment
// variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetDeprecatedBasicSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		// SNAP_
		"SNAP_APP_PATH={{.SnapPath}}",
		"SNAP_APP_DATA_PATH=/var{{.SnapPath}}",
	})
}

// GetDeprecatedUserSnapEnvVars returns the user-level deprecated environment
// variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetDeprecatedUserSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP_APP_USER_DATA_PATH={{.Home}}{{.SnapPath}}",
	})
}
