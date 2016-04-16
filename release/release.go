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
	"io/ioutil"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
)

var (
	// used in the unit tests
	lsbReleasePath = "/etc/lsb-release"
)

// Release contains a structure with the release information
type Release struct {
	Flavor string
	Series string
}

// Release is the current release
var rel Release

func init() {
	// we don't need to care for the error here to take into account when
	// initialized on a non snappy system
	Setup(dirs.GlobalRootDir)
}

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
	rel = Release{Flavor: "core", Series: "rolling"}

	return nil
}

// String returns the release information in a string which is valid to
// set for the store http headers.
func (r Release) String() string {
	return fmt.Sprintf("%s-%s", r.Series, r.Flavor)
}

// Lsb contains the /etc/lsb-release information of the system
type Lsb struct {
	ID       string
	Release  string
	Codename string
}

// ReadLsb returns the lsb-release information of the current system
func ReadLsb() (*Lsb, error) {
	lsb := &Lsb{}

	content, err := ioutil.ReadFile(lsbReleasePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read lsb-release: %s", err)
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "DISTRIB_ID=") {
			tmp := strings.SplitN(line, "=", 2)
			lsb.ID = tmp[1]
		}
		if strings.HasPrefix(line, "DISTRIB_RELEASE=") {
			tmp := strings.SplitN(line, "=", 2)
			lsb.Release = tmp[1]
		}
		if strings.HasPrefix(line, "DISTRIB_CODENAME=") {
			tmp := strings.SplitN(line, "=", 2)
			lsb.Codename = tmp[1]
		}
	}

	return lsb, nil
}
