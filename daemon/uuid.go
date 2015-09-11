// -*- Mode: Go; indent-tabs-mode: t -*-

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

package daemon

import (
	"fmt"
	"math/rand"
	"time"
)

// TODO: use an actual uuid package; these UUIDs actually have only
// 126 bits free (and less entropy than that). Good enough for our use
// case for now at least.

// An UUID is a universally unique identifier
type UUID [2]uint64

var source = rand.NewSource(time.Now().UTC().UnixNano())

// UUID4 are random-based UUIDs
func UUID4() UUID {
	return UUID{uint64(source.Int63()), uint64(source.Int63())}
}

func (u UUID) String() string {
	m := u[0]
	n := u[1]

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		n&0xffffffff,
		(n>>32)&0xffff,
		(n>>48)&0xffff,
		m&0xffff,
		m>>16)
}
