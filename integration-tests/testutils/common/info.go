// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package common

import (
	"bufio"
	"strings"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
)

// dependency aliasing
var execCommand = cli.ExecCommand

// Release returns the release of the current snappy image
func Release(c *check.C) string {
	info := execCommand(c, "snappy", "info")
	reader := strings.NewReader(info)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "release: ") {
			releaseInfo := strings.TrimPrefix(scanner.Text(), "release: ")
			if !strings.Contains(releaseInfo, "/") {
				return releaseInfo
			}

			return strings.Split(releaseInfo, "/")[1]
		}
	}
	c.Error("Release information not found")
	c.FailNow()
	return ""
}
