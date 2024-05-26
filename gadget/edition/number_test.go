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

package edition_test

import (
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/edition"
)

type editionSuite struct{}

var _ = Suite(&editionSuite{})

func TestCommand(t *testing.T) { TestingT(t) }

func (s *editionSuite) TestUnmarshalIntegration(c *C) {
	type editionStruct struct {
		Edition edition.Number `yaml:"edition"`
	}

	for _, tc := range []struct {
		input          string
		expectedNumber edition.Number
		expectedErr    string
	}{
		{"edition: 1", edition.Number(1), ""},
		{"edition: 0", edition.Number(0), ""},
		{"edition: 9999999", edition.Number(9999999), ""},
		{"edition: -1", edition.Number(0), `"edition" must be a positive number, not "-1"`},
		{"edition: random-string", edition.Number(0), `"edition" must be a positive number, not "random-string"`},
		{"edition: NaN", edition.Number(0), `"edition" must be a positive number, not "NaN"`},
		{"edition: 3.14", edition.Number(0), `"edition" must be a positive number, not "3.14"`},
	} {
		var en editionStruct
		mylog.Check(yaml.Unmarshal([]byte(tc.input), &en))
		if tc.expectedErr != "" {
			c.Assert(err, ErrorMatches, tc.expectedErr, Commentf(tc.input))
		} else {
			c.Assert(err, IsNil, Commentf(tc.input))
			c.Check(en.Edition, Equals, tc.expectedNumber, Commentf(tc.input))
		}
	}
}
