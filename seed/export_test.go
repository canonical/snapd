// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package seed

import (
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/testutil"
)

type InternalSnap16 = internal.Snap16

var LoadAssertions = loadAssertions

func MockOpen(f func(seedDir, label string) (Seed, error)) (restore func()) {
	r := testutil.Backup(&open)
	open = f
	return r
}

type TestSeed20 struct {
	*seed20
	Jobs int
}

func NewTestSeed20(s Seed) *TestSeed20 {
	return &TestSeed20{s.(*seed20), 0}
}

func (s *TestSeed20) SetParallelism(n int) {
	s.Jobs = n
}
