// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package errtracker

import (
	"bytes"
	"crypto/sha512"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"gopkg.in/mgo.v2/bson"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/release"
)

var (
	crashDbUrlBase = "https://daisy.ubuntu.com/"
	machineID      = "/var/lib/dbus/machine-id"

	timeNow = time.Now
)

// distroRelease returns a distro release as it is expected by daisy.ubuntu.com
func distroRelease() string {
	ID := release.ReleaseInfo.ID
	if ID == "ubuntu" {
		ID = "Ubuntu"
	}

	return fmt.Sprintf("%s %s", ID, release.ReleaseInfo.VersionID)
}

func Report(snap, channel, errMsg string) (string, error) {
	machineID, err := ioutil.ReadFile(machineID)
	if err != nil {
		return "", err
	}
	machineID = bytes.TrimSpace(machineID)
	identifier := fmt.Sprintf("%x", sha512.Sum512(machineID))

	crashDbUrl := fmt.Sprintf("%s/%s", crashDbUrlBase, identifier)

	report := map[string]string{
		"ProblemType":        "Snap",
		"Architecture":       arch.UbuntuArchitecture(),
		"DistroRelease":      distroRelease(),
		"Date":               timeNow().Format(time.ANSIC),
		"Snap":               snap,
		"Channel":            channel,
		"ErrorMessage":       errMsg,
		"DuplicateSignature": fmt.Sprintf("snap-install: %s", errMsg),
	}
	reportBson, err := bson.Marshal(report)
	if err != nil {
		return "", err
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", crashDbUrl, bytes.NewBuffer(reportBson))
	req.Header.Add("Content-Type", "application/octet-stream")
	req.Header.Add("X-Whoopsie-Version", httputil.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("cannot upload error report, return code: %d", resp.StatusCode)
	}
	oopsID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(oopsID), nil
}
