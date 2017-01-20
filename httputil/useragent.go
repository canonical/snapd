// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package httputil

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// UserAgent to send
// TODO: this should actually be set per client request, and include the client user agent
var userAgent = "unset"

var isTesting bool

func init() {
	if osutil.GetenvBool("SNAPPY_TESTING") {
		isTesting = true
	}
}

// SetUserAgentFromVersion sets up a user-agent string.
func SetUserAgentFromVersion(version string, extraProds ...string) {
	extras := make([]string, 1, 3)
	extras[0] = "series " + release.Series
	if release.OnClassic {
		extras = append(extras, "classic")
	}
	if release.ReleaseInfo.ForceDevMode() {
		extras = append(extras, "devmode")
	}
	if isTesting {
		extras = append(extras, "testing")
	}
	extraProdStr := ""
	if len(extraProds) != 0 {
		extraProdStr = " " + strings.Join(extraProds, " ")
	}
	// xxx this assumes ReleaseInfo's ID and VersionID don't have weird characters
	// (see rfc 7231 for values of weird)
	// assumption checks out in practice, q.v. https://github.com/zyga/os-release-zoo
	userAgent = fmt.Sprintf("snapd/%v (%s)%s %s/%s (%s)", version, strings.Join(extras, "; "), extraProdStr, release.ReleaseInfo.ID, release.ReleaseInfo.VersionID, string(arch.UbuntuArchitecture()))
}

// UserAgent returns the user-agent string setup through SetUserAgentFromVersion.
func UserAgent() string {
	return userAgent
}
