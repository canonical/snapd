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

package release

import (
	"fmt"
)

// Release contains a structure with the release information
type Release struct {
	Flavor  string
	Series  string
	Channel string
}

var rel Release

// String returns the release information in a string
func String() string {
	return rel.String()
}

// Get the release
func Get() Release {
	return rel
}

// Override sets up the release using a Release
func Override(r Release) {
	rel = r
}

// Setup is used to initialiaze the release information for the system
func Setup(rootDir string) error {
	rel = Release{Flavor: "core", Series: "rolling", Channel: "edge"}

	return nil
}

// String returns the release information in a string which is valid to
// set for the store http headers.
func (r Release) String() string {
	return fmt.Sprintf("%s-%s", r.Series, r.Flavor)
}
