// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package osutil

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var mountInfoPath = "/proc/self/mountinfo"

func IsMounted(baseDir string) (bool, error) {
	f, err := os.Open(mountInfoPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		l := strings.Fields(scanner.Text())
		if len(l) == 0 {
			continue
		}
		if len(l) < 7 {
			return false, fmt.Errorf("unexpected mountinfo line: %q", scanner.Text())
		}
		// this parser is simplistic, there are optional fields in
		// the mountinfo lines, however those are *after* l[4] so
		// we ignore it for now (because we only care about the
		// mount point)
		mountPoint := l[4]
		if baseDir == mountPoint {
			return true, nil
		}
	}

	return false, scanner.Err()
}
