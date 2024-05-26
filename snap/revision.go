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

	"github.com/ddkwork/golibrary/mylog"
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
	return strconv.Itoa(r.N)
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
	return []byte(`"` + r.String() + `"`), nil
}

func (r *Revision) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	mylog.Check(unmarshal(&s))

	return r.UnmarshalJSON([]byte(`"` + s + `"`))
}

func (r Revision) MarshalYAML() (interface{}, error) {
	return r.String(), nil
}

func (r *Revision) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' && data[len(data)-1] == '"' {
		parsed := mylog.Check2(ParseRevision(string(data[1 : len(data)-1])))
		if err == nil {
			*r = parsed
			return nil
		}
	} else {
		n := mylog.Check2(strconv.ParseInt(string(data), 10, 64))
		if err == nil {
			r.N = int(n)
			return nil
		}
	}
	return fmt.Errorf("invalid snap revision: %q", data)
}

// ParseRevision returns the representation in r as a revision.
// See R for a function more suitable for hardcoded revisions.
func ParseRevision(s string) (Revision, error) {
	if s == "unset" {
		return Revision{}, nil
	}
	if s != "" && s[0] == 'x' {
		i := mylog.Check2(strconv.Atoi(s[1:]))
		if err == nil && i > 0 {
			return Revision{-i}, nil
		}
	}
	i := mylog.Check2(strconv.Atoi(s))
	if err == nil && i > 0 {
		return Revision{i}, nil
	}
	return Revision{}, fmt.Errorf("invalid snap revision: %#v", s)
}

// R returns a Revision given an int or a string.
// Providing an invalid revision type or value causes a runtime panic.
// See ParseRevision for a polite function that does not panic.
func R(r interface{}) Revision {
	switch r := r.(type) {
	case string:
		revision := mylog.Check2(ParseRevision(r))

		return revision
	case int:
		return Revision{r}
	default:
		panic(fmt.Errorf("cannot use %v (%T) as a snap revision", r, r))
	}
}
