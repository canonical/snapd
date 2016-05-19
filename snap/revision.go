// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap

import (
	"fmt"
	"strconv"
)

// Keep this in sync between snap and client packages.

type Revision struct {
	N int
}

func (r Revision) String() string {
	if r.N == 0 {
		return "unset"
	}
	if r.N < 0 {
		return fmt.Sprintf("x%d", -r.N)
	}
	return strconv.Itoa(int(r.N))
}

func (r Revision) Unset() bool {
	return r.N == 0
}

func (r Revision) Local() bool {
	return r.N < 0
}

func (r Revision) Store() bool {
	return r.N > 0
}

func (r Revision) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, int64(r.N), 10), nil
}

func (r *Revision) UnmarshalJSON(data []byte) error {
	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid snap revision: %q", data)
	}
	r.N = int(n)
	return nil
}
