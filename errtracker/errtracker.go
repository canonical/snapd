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
	"os"
	"path/filepath"
	"time"

	"gopkg.in/mgo.v2/bson"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

var (
	CrashDbURLBase string
	SnapdVersion   string

	// The machine-id file is at different locations depending on how the system
	// is setup. On Fedora for example /var/lib/dbus/machine-id doesn't exist
	// but we have /etc/machine-id. See
	// https://www.freedesktop.org/software/systemd/man/machine-id.html for a
	// few more details.
	machineIDs = []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}

	mockedHostSnapd = ""
	mockedCoreSnapd = ""

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

func readMachineID() ([]byte, error) {
	for _, id := range machineIDs {
		machineID, err := ioutil.ReadFile(id)
		if err == nil {
			return bytes.TrimSpace(machineID), nil
		} else if !os.IsNotExist(err) {
			logger.Noticef("cannot read %s: %s", id, err)
		}
	}

	return nil, fmt.Errorf("cannot report: no suitable machine id file found")
}

func Report(snap, errMsg, dupSig string, extra map[string]string) (string, error) {
	if CrashDbURLBase == "" {
		return "", nil
	}

	machineID, err := readMachineID()
	if err != nil {
		return "", err
	}

	identifier := fmt.Sprintf("%x", sha512.Sum512(machineID))

	crashDbUrl := fmt.Sprintf("%s/%s", CrashDbURLBase, identifier)

	hostSnapdPath := filepath.Join(dirs.DistroLibExecDir, "snapd")
	coreSnapdPath := filepath.Join(dirs.SnapMountDir, "core/current/usr/lib/snapd/snapd")
	if mockedHostSnapd != "" {
		hostSnapdPath = mockedHostSnapd
	}
	if mockedCoreSnapd != "" {
		coreSnapdPath = mockedCoreSnapd
	}
	hostBuildID, _ := osutil.ReadBuildID(hostSnapdPath)
	coreBuildID, _ := osutil.ReadBuildID(coreSnapdPath)
	if hostBuildID == "" {
		hostBuildID = "unknown"
	}
	if coreBuildID == "" {
		coreBuildID = "unknown"
	}

	report := map[string]string{
		"ProblemType":        "Snap",
		"Architecture":       arch.UbuntuArchitecture(),
		"SnapdVersion":       SnapdVersion,
		"DistroRelease":      distroRelease(),
		"HostSnapdBuildID":   hostBuildID,
		"CoreSnapdBuildID":   coreBuildID,
		"Date":               timeNow().Format(time.ANSIC),
		"Snap":               snap,
		"KernelVersion":      release.KernelVersion(),
		"ErrorMessage":       errMsg,
		"DuplicateSignature": dupSig,
	}
	for k, v := range extra {
		// only set if empty
		if _, ok := report[k]; !ok {
			report[k] = v
		}
	}

	// see if we run in testing mode
	if osutil.GetenvBool("SNAPPY_TESTING") {
		logger.Noticef("errtracker.Report is *not* sent because SNAPPY_TESTING is set")
		logger.Noticef("report: %v", report)
		return "oops-not-sent", nil
	}

	// send it for real
	reportBson, err := bson.Marshal(report)
	if err != nil {
		return "", err
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", crashDbUrl, bytes.NewBuffer(reportBson))
	if err != nil {
		return "", err
	}
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
