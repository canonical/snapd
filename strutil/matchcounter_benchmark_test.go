// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package strutil_test

import (
	"regexp"
	"testing"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

func benchmarkMatchCounter(b *testing.B, wrx *regexp.Regexp, wn int) {
	buf := []byte(out)
	for n := 0; n < b.N; n++ {
		for step := 1; step < 100; step++ {
			w := &strutil.MatchCounter{Regexp: wrx, N: wn}
			var i int
			for i = 0; i+step < len(buf); i += step {
				_ := mylog.Check2(w.Write(buf[i : i+step]))
			}
			_ := mylog.Check2(w.Write(buf[i:]))

		}
	}
}

func BenchmarkNil(b *testing.B)         { benchmarkMatchCounter(b, nil, 3) }
func BenchmarkNilAll(b *testing.B)      { benchmarkMatchCounter(b, nil, -1) }
func BenchmarkNilEquiv(b *testing.B)    { benchmarkMatchCounter(b, nilRegexpEquiv, 3) }
func BenchmarkNilEquivAll(b *testing.B) { benchmarkMatchCounter(b, nilRegexpEquiv, -1) }
