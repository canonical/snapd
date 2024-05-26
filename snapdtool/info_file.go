// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package snapdtool

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

// SnapdVersionFromInfoFile returns the snapd version read from the info file in
// the given dir, as well as any other key/value pairs/flags in the file.
// See ParseInfoFile for more format details.
func SnapdVersionFromInfoFile(dir string) (version string, flags map[string]string, err error) {
	infoPath := filepath.Join(dir, "info")
	f := mylog.Check2(os.Open(infoPath))

	defer f.Close()

	return ParseInfoFile(f, fmt.Sprintf("%q", infoPath))
}

// ParseInfoFile parses the "info" file provided via an io.Reader. It returns
// the snapd version read from the info file, as well as any other key/value
// pairs/flags in the file.
// whence is used to construct error messages as "... info file %s".
// The format of the "info" file are lines with "KEY=VALUE" with the typical key
// being just VERSION. The file is produced by mkversion.sh and normally
// installed along snapd binary in /usr/lib/snapd.
// Other typical keys in this file include SNAPD_APPARMOR_REEXEC, which
// indicates whether or not the snapd-apparmor binary installed via the
// traditional linux package of snapd supports re-exec into the version in the
// snapd or core snaps.
func ParseInfoFile(f io.Reader, whence string) (version string, flags map[string]string, err error) {
	flags = map[string]string{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VERSION=") {
			version = strings.TrimPrefix(line, "VERSION=")
		} else {
			keyVal := strings.SplitN(line, "=", 2)
			if len(keyVal) != 2 {
				// potentially malformed line, just skip it
				continue
			}

			flags[keyVal[0]] = keyVal[1]
		}
	}
	mylog.Check(scanner.Err())

	if version == "" {
		return "", nil, fmt.Errorf("cannot find version in snapd info file %s", whence)
	}

	return version, flags, nil
}
