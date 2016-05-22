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

	"github.com/ubuntu-core/snappy/osutil"
)

// Series holds the Ubuntu Core series for snapd to use.
var Series = "16"

// LSB contains the /etc/os-release information of the system.
type LSB struct {
	ID       string
	Release  string
	Codename string
}

var lsbReleasePath = "/etc/os-release"

// ReadLSB returns the os-release information of the current system.
func ReadLSB() (*LSB, error) {
	lsb := &LSB{}

	content, err := ioutil.ReadFile(lsbReleasePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read os-release: %s", err)
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "NAME=") {
			tmp := strings.SplitN(line, "=", 2)
			lsb.ID = strings.Trim(tmp[1],"\"")
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			tmp := strings.SplitN(line, "=", 2)
			lsb.Release = strings.Trim(tmp[1],"\"")
		}
		if strings.HasPrefix(line, "UBUNTU_CODENAME=") {
			tmp := strings.SplitN(line, "=", 2)
			lsb.Codename = strings.Trim(tmp[1],"\"")
		}
	}
	if (lsb.Codename == "") {
		lsb.Codename = "xenial"
	}

	return lsb, nil
}

// OnClassic states whether the process is running inside a
// classic Ubuntu system or a native Ubuntu Core image.
var OnClassic bool

func init() {
	OnClassic = osutil.FileExists("/var/lib/dpkg/status")
}

// MockOnClassic forces the process to appear inside a classic
// Ubuntu system or a native image for testing purposes.
func MockOnClassic(onClassic bool) (restore func()) {
	old := OnClassic
	OnClassic = onClassic
	return func() { OnClassic = old }
}
