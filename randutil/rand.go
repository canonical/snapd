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
	cryptorand "crypto/rand"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"time"
)

func init() {
	// golang does not init Seed() itself
	bigSeed, err := cryptorand.Int(cryptorand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(fmt.Sprintf("cannot obtain random seed: %v", err))
	}
	rand.Seed(bigSeed.Int64())
}

const letters = "BCDFGHJKLMNPQRSTVWXYbcdfghjklmnpqrstvwxy0123456789"

// MakeRandomString returns a random string of length length
//
// The vowels are omitted to avoid that words are created by pure
// chance. Numbers are included.
//
// Not cryptographically safe.
func MakeRandomString(length int) string {
	out := ""
	for i := 0; i < length; i++ {
		out += string(letters[rand.Intn(len(letters))])
	}

	return out
}

// Rexported from math/rand for streamlining.
var (
	Intn   = rand.Intn
	Int63n = rand.Int63n
)

// RandomDuration returns a random duration up to the given length.
func RandomDuration(d time.Duration) time.Duration {
	return time.Duration(Int63n(int64(d)))
}
