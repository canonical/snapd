// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package keyring

import (
	"github.com/ddkwork/golibrary/mylog"
	"golang.org/x/sys/unix"
)

const (
	userKeyType = "user"
	userKeyring = -4
)

func formatDesc(devicePath, purpose, prefix string) string {
	return prefix + ":" + devicePath + ":" + purpose
}

func AddKeyToUserKeyring(key []byte, devicePath, purpose, prefix string) error {
	_ := mylog.Check2(unix.AddKey(userKeyType, formatDesc(devicePath, purpose, prefix), key, userKeyring))
	return err
}
