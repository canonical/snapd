// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

// Package randutil initialises properly random value generation and
// exposes a streamlined set of functions for it, including for crypto
// random tokens.
package randutil

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"

	"github.com/ddkwork/golibrary/mylog"
)

func init() {
	// golang does not init Seed() itself
	rand.Seed(time.Now().UnixNano() + int64(os.Getpid()))
}

var moreMixedSeedOnce sync.Once

func moreMixedSeed() {
	moreMixedSeedOnce.Do(func() {
		h := sha256.New224()
		// do this instead of asking for time and pid again
		var b [8]byte
		rand.Read(b[:])
		h.Write(b[:])
		// mix in the hostname
		if hostname := mylog.Check2(os.Hostname()); err == nil {
			h.Write([]byte(hostname))
		}
		// mix in net interfaces hw addresses (MACs etc)
		if ifaces := mylog.Check2(net.Interfaces()); err == nil {
			for _, iface := range ifaces {
				h.Write(iface.HardwareAddr)
			}
		}
		hs := h.Sum(nil)
		s := binary.LittleEndian.Uint64(hs[0:])
		rand.Seed(int64(s))
	})
}

const letters = "BCDFGHJKLMNPQRSTVWXYbcdfghjklmnpqrstvwxy0123456789"

// RandomString returns a random string of length length.
//
// The vowels are omitted to avoid that words are created by pure
// chance. Numbers are included.
//
// Not cryptographically secure.
func RandomString(length int) string {
	out := ""
	for i := 0; i < length; i++ {
		out += string(letters[rand.Intn(len(letters))])
	}

	return out
}

// Re-exported from math/rand for streamlining.
var (
	Intn   = rand.Intn
	Int63n = rand.Int63n
)

// RandomDuration returns a random duration up to the given length.
func RandomDuration(d time.Duration) time.Duration {
	// try to switch to more mixed seed to avoid subsets of a
	// fleet of machines with similar initial conditions to behave
	// the same
	moreMixedSeed()
	return time.Duration(Int63n(int64(d)))
}
