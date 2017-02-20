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

	"labix.org/v2/mgo/bson"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/httputil"
)

var (
	crashDbUrlBase = "https://daisy.ubuntu.com/"
	machineID      = "/var/lib/dbus/machine-id"

	timeNow = time.Now
)

func Report(snap, channel, errMsg string) error {
	machineID, err := ioutil.ReadFile(machineID)
	if err != nil {
		return err
	}
	identifier := fmt.Sprintf("%x", sha512.Sum512(machineID))

	crashDbUrl := fmt.Sprintf("%s/%s", crashDbUrlBase, identifier)

	report := map[string]string{
		"ProblemType":  "Snap",
		"Architecture": arch.UbuntuArchitecture(),
		"Date":         fmt.Sprintf("%s", timeNow()),
		"Snap":         snap,
		"Channel":      channel,
		"ErrorMessage": errMsg,
	}
	reportBson, err := bson.Marshal(report)
	if err != nil {
		return err
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", crashDbUrl, bytes.NewBuffer(reportBson))
	req.Header.Add("Content-Type", "application/octet-stream")
	req.Header.Add("X-Whoopsie-Version", httputil.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("cannot upload error report, return code: %d", resp.StatusCode)
	}

	return nil
}
