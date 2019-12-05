// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package cmdutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
)

// SnapdVersionFromInfoFile returns snapd version read for the
// given info" file, pointed by infoPath.
// The format of the "info" file is a single line with "VERSION=..."
// in it. The file is produced by mkversion.sh and normally installed
// along snapd binary in /usr/lib/snapd.
func SnapdVersionFromInfoFile(infoPath string) (string, error) {
	content, err := ioutil.ReadFile(infoPath)
	if err != nil {
		return "", fmt.Errorf("cannot open snapd info file %q: %s", infoPath, err)
	}

	if !bytes.HasPrefix(content, []byte("VERSION=")) {
		idx := bytes.Index(content, []byte("\nVERSION="))
		if idx < 0 {
			return "", fmt.Errorf("cannot find snapd version information in %q", content)
		}
		content = content[idx+1:]
	}
	content = content[8:]
	idx := bytes.IndexByte(content, '\n')
	if idx > -1 {
		content = content[:idx]
	}

	return string(content), nil
}
