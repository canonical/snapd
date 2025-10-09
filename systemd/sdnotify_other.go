// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !linux

/*
 * Copyright (C) 2017-2025 Canonical Ltd
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

package systemd

import (
	"errors"
)

var errUnsupported = errors.New("unsupported on non-Linux systems")

var SdNotify = func(notifyState string) error {
	return errUnsupported
}

var SdNotifyWithFds = func(notifyState string, fds ...int) error {
	return errUnsupported
}
