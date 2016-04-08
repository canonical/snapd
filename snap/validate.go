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

package snap

import (
	"fmt"
	"regexp"
)

// Regular expression describing correct identifiers.
var validName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateName checks if a string can be used as a snap name.
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid snap name: %q", name)
	}
	return nil
}

// Validate verifies the content in the info.
func Validate(info *Info) error {
	return nil
}

func validateField(name, cont string, whitelist *regexp.Regexp) error {
	if !whitelist.MatchString(cont) {
		return fmt.Errorf("app description field '%s' contains illegal %q (legal: '%s')", name, cont, whitelist)

	}
	return nil
}

// appContentWhitelist is the whitelist of legal chars in the "apps"
// section of snap.yaml
var appContentWhitelist = regexp.MustCompile(`^[A-Za-z0-9/. _#:-]*$`)

// ValidateApp verifies the content in the app info.
func ValidateApp(app *AppInfo) error {
	switch app.Daemon {
	case "", "simple", "forking", "oneshot", "dbus":
		// valid
	default:
		return fmt.Errorf(`"daemon" field contains invalid value %q`, app.Daemon)
	}

	checks := map[string]string{
		"name":              app.Name,
		"command":           app.Command,
		"stop-command":      app.Stop,
		"post-stop-command": app.PostStop,
		"socket-mode":       app.SocketMode,
		"listen-stream":     app.ListenStream,
		"bus-name":          app.BusName,
	}

	for name, value := range checks {
		if err := validateField(name, value, appContentWhitelist); err != nil {
			return err
		}
	}
	return nil
}
