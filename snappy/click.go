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

package snappy

import (
	"github.com/ubuntu-core/snappy/progress"
)

// FIXME: kill once every test is converted
func installClick(snapFilePath string, flags InstallFlags, inter progress.Meter, developer string) (name string, err error) {
	overlord := &Overlord{}
	snapPart, err := overlord.Install(snapFilePath, developer, flags, inter)
	if err != nil {
		return "", err
	}

	return snapPart.Name(), nil
}
