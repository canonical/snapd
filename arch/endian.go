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

package arch

import (
	"encoding/binary"
	"fmt"
	"runtime"
)

var runtimeGOARCH = runtime.GOARCH

// Endian will return the native endianness of the system
func Endian() binary.ByteOrder {
	switch runtimeGOARCH {
	case "ppc", "ppc64", "s390x":
		return binary.BigEndian
	case "386", "amd64", "arm", "arm64", "ppc64le", "riscv64":
		return binary.LittleEndian
	default:
		panic(fmt.Sprintf("unknown architecture %s", runtimeGOARCH))
	}
}
