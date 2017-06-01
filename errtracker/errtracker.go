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
	"crypto/md5"
	"crypto/sha512"
	"fmt"
	"io/ioutil"
	"net/http"
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

	machineID       = "/var/lib/dbus/machine-id"
	mockedHostSnapd = ""
	mockedCoreSnapd = ""

	snapConfineProfile = "/etc/apparmor.d/usr.lib.snapd.snap-confine.real"

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

func snapConfineProfileDigest(suffix string) string {
	profileText, err := ioutil.ReadFile(snapConfineProfile + suffix)
	if err != nil {
		return ""
	}
	// NOTE: uses md5sum for easier comparison against dpkg meta-data
	return fmt.Sprintf("%x", md5.Sum(profileText))
}

func didSnapdReExec() string {
	if osutil.GetenvBool("SNAP_DID_REEXEC") {
		return "yes"
	}
	return "no"
}

func Report(snap, errMsg, dupSig string, extra map[string]string) (string, error) {
	if CrashDbURLBase == "" {
		return "", nil
	}

	machineID, err := ioutil.ReadFile(machineID)
	if err != nil {
		return "", err
	}
	machineID = bytes.TrimSpace(machineID)
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

		"SnapConfineAppArmorProfileCurrentMD5Sum": snapConfineProfileDigest(""),
		"SnapConfineAppArmorProfileDpkgNewMD5Sum": snapConfineProfileDigest(".dpkg-new"),

		"DidSnapdReExec": didSnapdReExec(),
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
