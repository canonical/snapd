// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"testing"

	"github.com/snapcore/snapd/strutil"
)

var versions = []string{
	"~",
	"",
	"0",
	"00",
	"009",
	"009ab5",
	"0.10.0",
	"0.2",
	"0.4",
	"0.4-1",
	"0.4a6",
	"0.4a6-2",
	"0.5.0~git",
	"0.5.0~git2",
	"0.8.7",
	"0-pre",
	"0pre",
	"0-pree",
	"0pree",
	"1.0~",
	"1.0",
	"1.0-0~",
	"1.0-0",
	"1.00",
	"1.002-1+b2",
	"1.0-0+b1",
	"1.0-1",
	"1.0-1.1",
	"1.0-2",
	"1.1.6r-1",
	"1.1.6r2-2",
	"1.18.36:5.4",
	"1.18.36:5.5",
	"1.18.37:1.1",
	"1.2.2.2",
	"1.2.24",
	"1.2.3",
	"1.2.3-0",
	"1.2.3-1",
	"1.2.4",
	"1.2a+~",
	"1.2a++",
	"1.2a+~bCd3",
	"1.3",
	"1.3.1",
	"1.3.2",
	"1.3.2a",
	"1.4+OOo3.0.0~",
	"1.4+OOo3.0.0-4",
	"2.0",
	"2.0.7pre1",
	"2.0.7r",
	"21",
	"2.3",
	"2.4.7-1",
	"2.4.7-z",
	"2.6b-2",
	"2.6b2-1",
	"2a",
	"3.0-1",
	"3.0~rc1-1",
	"3~10",
	"3.10.2",
	"3.2",
	"3a9.8",
	"4.4.3-2",
	"5.005",
	"5.10.0",
	"7.2",
	"7.2p2",
	"9",
	"9ab5",
}

func BenchmarkVersionCompare(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for i := range versions {
			for j := range versions {
				strutil.VersionCompare(versions[i], versions[j])
			}
		}
	}
}
