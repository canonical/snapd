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

// Package randutil exposes a streamlined set of randomisation helper
// functions, including for crypto random tokens. Pseudo random based
// functions avoid using the math/rand global RNG as it is impossible
// to track seed changes globally. The global RNG is also automatically
// seeded from Go v1.20 onwards.
package randutil

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"
)

// SeedDatePid provides a basic seed value based only on
// time and os PID value.
func SeedDatePid() int64 {
	return time.Now().UnixNano() + int64(os.Getpid())
}

// SeedDatePidHostMac provides a seed value that in addition to
// time and os PID, also takes into account the device hostname
// and network inteface MAC addresses. This may be required when
// you are trying to randomize the time of actions within a
// fleet of similar devices, where the time and PID values may
// not provide enough variance within the device pool.
func SeedDatePidHostMac() int64 {
	// Use a pseudo RNG initially for time and pid inclusion
	var b [8]byte
	pr := NewPseudoRand(SeedDatePid())
	pr.rand.Read(b[:])

	h := sha256.New224()
	// Mix in the time and pid
	h.Write(b[:])
	// Mix in the hostname
	if hostname, err := os.Hostname(); err == nil {
		h.Write([]byte(hostname))
	}
	// Mix in net interfaces hw addresses (MACs etc)
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			h.Write(iface.HardwareAddr)
		}
	}
	hs := h.Sum(nil)
	s := binary.LittleEndian.Uint64(hs[0:])
	return int64(s)
}

// PseudoRand provides a go-routine safe randomisation helper methods.
type PseudoRand struct {
	rand *rand.Rand
	lk   sync.Mutex
}

// NewPseudoRand returns a new pseudo RNG instance.
func NewPseudoRand(seed int64) *PseudoRand {
	return &PseudoRand{rand: rand.New(rand.NewSource(seed))}
}

// Reseed is exposed to allow tests to reseed the pseudo RNG to
// allow for deterministic results.
func (r *PseudoRand) Reseed(seed int64) {
	r.lk.Lock()
	defer r.lk.Unlock()

	r.rand.Seed(seed)
}

const letters = "BCDFGHJKLMNPQRSTVWXYbcdfghjklmnpqrstvwxy0123456789"

// RandomString returns a random string of length length.
//
// The vowels are omitted to avoid that words are created by pure
// chance. Numbers are included.
//
// Not cryptographically secure.
func (r *PseudoRand) RandomString(length int) string {
	r.lk.Lock()
	defer r.lk.Unlock()

	out := ""
	for i := 0; i < length; i++ {
		out += string(letters[r.rand.Intn(len(letters))])
	}

	return out
}

// RandomDuration returns a positive random duration in the half-open positive
// interval [0,d). Any zero or negative input duration results in a return
// of zero duration.
func (r *PseudoRand) RandomDuration(d time.Duration) time.Duration {
	r.lk.Lock()
	defer r.lk.Unlock()

	// Prevent a panic on <= 0, rather return 0
	if d <= 0 {
		return time.Duration(0)
	}

	return time.Duration(r.rand.Int63n(int64(d)))
}
