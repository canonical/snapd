// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package internal_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seed/internal"
)

type validateSuite struct{}

var _ = Suite(&validateSuite{})

func (s *validateSuite) TestValidateSeedSystemLabel(c *C) {
	valid := []string{
		"a",
		"ab",
		"a-a",
		"a-123",
		"a-a-a",
		"20191119",
		"foobar",
		"my-system",
		"brand-system-date-1234",
	}
	for _, label := range valid {
		c.Logf("trying valid label: %q", label)
		err := internal.ValidateUC20SeedSystemLabel(label)
		c.Check(err, IsNil)
	}

	invalid := []string{
		"",
		"/bin",
		"../../bin/bar",
		":invalid:",
		"日本語",
		"-invalid",
		"invalid-",
		"MYSYSTEM",
		"mySystem",
	}
	for _, label := range invalid {
		c.Logf("trying invalid label: %q", label)
		err := internal.ValidateUC20SeedSystemLabel(label)
		c.Check(err, ErrorMatches, fmt.Sprintf("invalid seed system label: %q", label))
	}
}
