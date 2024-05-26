// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package naming

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
)

var coreNameFormat = regexp.MustCompile("^core(?P<version>[0-9]*)(?:-.*)?$")

// CoreVersion extract the version component of the core snap name
// Most core snap names are of the form coreXX where XX is a number.
// CoreVersion returns that number. In case of "core", it returns
// 16.
func CoreVersion(base string) (int, error) {
	foundCore := coreNameFormat.FindStringSubmatch(base)

	if foundCore == nil {
		return 0, fmt.Errorf("not a core base")
	}

	coreVersionStr := foundCore[coreNameFormat.SubexpIndex("version")]

	if coreVersionStr == "" {
		return 16, nil
	}

	v := mylog.Check2(strconv.Atoi(coreVersionStr))

	// Unreachable

	return v, nil
}
