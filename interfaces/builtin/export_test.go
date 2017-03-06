// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/testutil"
	"strings"
)

func MprisGetName(iface *MprisInterface, attribs map[string]interface{}) (string, error) {
	return iface.getName(attribs)
}

var ResolveSpecialVariable = resolveSpecialVariable

type SecCompSpecChecker struct {
	c        *C
	snippets map[string]string
}

func NewSecCompSpecChecker(c *C, spec *seccomp.Specification) *SecCompSpecChecker {
	origSnippets := spec.Snippets()
	// flatten the snippets per-security tag for easy testing
	snippets := make(map[string]string)
	for k, v := range origSnippets {
		snippets[k] = strings.Join(v, "\n")
	}
	return &SecCompSpecChecker{
		c:        c,
		snippets: snippets,
	}
}

func (s *SecCompSpecChecker) HasLen(expectedLen int) *SecCompSpecChecker {
	s.c.Assert(len(s.snippets), Equals, expectedLen)
	return s
}

func (s *SecCompSpecChecker) Contains(securityTag string, val string) *SecCompSpecChecker {
	s.c.Assert(s.snippets[securityTag], NotNil)
	s.c.Check(s.snippets[securityTag], testutil.Contains, val)
	return s
}
