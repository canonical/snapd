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
	"os"
	"strings"
)

// Series holds the Ubuntu Core series for snapd to use.
var Series = "16"

// LSB contains the /etc/lsb-release information of the system.
type LSB struct {
	ID       string
	Release  string
	Codename string
}

var lsbReleasePath = "/etc/lsb-release"

// ReadLSB returns the lsb-release information of the current system.
func ReadLSB() (*LSB, error) {
	lsb := &LSB{}

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

var dpkgStatusPath = "/var/lib/dpkg/status"

// OnClassic returns whether the process is running inside a
// classic Ubuntu system or a native Ubuntu Core image.
func OnClassic() bool {
	if _, err := os.Stat(dpkgStatusPath); err == nil {
		return true
	}
	return false
}

// MockOnClassic forces the process to appear inside a classic
// Ubuntu system or a native image for testing purposes.
func MockOnClassic(onClassic bool) (restore func()) {
	old := dpkgStatusPath
	if onClassic {
		dpkgStatusPath = "/"
	} else {
		dpkgStatusPath = "/non-existent"
	}
	return func() { dpkgStatusPath = old }
}
