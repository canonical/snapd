// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"net"

	"github.com/snapcore/snapd/testutil"
)

var PeerIsConfinedSnapCheck = peerIsConfinedSnap

func MockGetsockoptPeerSec(f func(*net.UnixConn) (string, error)) (restore func()) {
	restore = testutil.Backup(&getsockoptPeerSec)
	getsockoptPeerSec = f
	return restore
}

func MockMaxMessageSize(size int) (restore func()) {
	old := maxVarlinkMessageSize
	maxVarlinkMessageSize = size
	return func() {
		maxVarlinkMessageSize = old
	}
}
