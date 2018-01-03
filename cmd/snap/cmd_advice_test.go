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

package main_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/advisor"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type sillyFinder struct{}

func (sf *sillyFinder) Find(command string) ([]string, error) {
	switch command {
	case "hello":
		return []string{"hello", "hello-wcm"}, nil
	case "error-please":
		return nil, fmt.Errorf("get failed")
	default:
		return nil, nil
	}
}

func (s *SnapSuite) TestAdviceCommandHappyText(c *C) {
	restore := advisor.ReplaceCommandsFinder(&sillyFinder{})
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"advice-command", "hello"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, `The program "hello" can be found in the following snaps:
 * hello
 * hello-wcm
Try: snap install <selected snap>
`)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAdviceCommandHappyJSON(c *C) {
	restore := advisor.ReplaceCommandsFinder(&sillyFinder{})
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"advice-command", "--format=json", "hello"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, `["hello","hello-wcm"]`+"\n")
	c.Assert(s.Stderr(), Equals, "")
}
