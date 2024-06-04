// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build ppc64le && go1.7 && !go1.8

/*
 * Copyright (C) 2017 Canonical Ltd
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

/*
#cgo LDFLAGS: -no-pie

// we need "-no-pie" for ppc64le,go1.7 to work around build failure on
// ppc64el with go1.7, see
// https://forum.snapcraft.io/t/snapd-master-fails-on-zesty-ppc64el-with-r-ppc64-addr16-ha-for-symbol-out-of-range/
*/
import "C"
