// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !linux

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

package main

import (
	"errors"
)

var preseedNotAvailableError = errors.New("preseed mode not available for systems other than linux")

func checkChroot(preseedChroot string) error {
	return preseedNotAvailableError
}

func prepareChroot(preseedChroot string) (*targetSnapdInfo, func(), error) {
	return nil, preseedNotAvailableError
}

func runPreseedMode(rootDir string) error {
	return preseedNotAvailableError
}

func cleanup() {}
