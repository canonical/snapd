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
	"fmt"
)

func checkChroot(preseedChroot string) error {
	return fmt.Errorf("preseed mode not available for systems other than linux")
}

func prepareChroot(preseedChroot string) (func(), error) {
	return nil, fmt.Errorf("preseed mode not available for systems other than linux")
}

func runPreseedMode(rootDir string) error {
	return fmt.Errorf("preseed mode not available for systems other than linux")
}

func cleanup() {}
