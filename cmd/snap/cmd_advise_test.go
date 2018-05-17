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

func mkSillyFinder() (advisor.Finder, error) {
	return &sillyFinder{}, nil
}

func (sf *sillyFinder) FindCommand(command string) ([]advisor.Command, error) {
	switch command {
	case "hello":
		return []advisor.Command{
			{Snap: "hello", Command: "hello"},
			{Snap: "hello-wcm", Command: "hello"},
		}, nil
	case "error-please":
		return nil, fmt.Errorf("get failed")
	default:
		return nil, nil
	}
}

func (sf *sillyFinder) FindPackage(pkgName string) (*advisor.Package, error) {
	switch pkgName {
	case "hello":
		return &advisor.Package{Snap: "hello", Summary: "summary for hello"}, nil
	case "error-please":
		return nil, fmt.Errorf("find-pkg failed")
	default:
		return nil, nil
	}
}

func (*sillyFinder) Close() error { return nil }

func (s *SnapSuite) TestAdviseCommandHappyText(c *C) {
	restore := advisor.ReplaceCommandsFinder(mkSillyFinder)
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"advise-snap", "--command", "hello"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, `
Command "hello" not found, but can be installed with:

sudo snap install hello
sudo snap install hello-wcm

See 'snap info <snap name>' for additional versions.

`)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAdviseCommandHappyJSON(c *C) {
	restore := advisor.ReplaceCommandsFinder(mkSillyFinder)
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"advise-snap", "--command", "--format=json", "hello"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, `[{"Snap":"hello","Command":"hello"},{"Snap":"hello-wcm","Command":"hello"}]`+"\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAdviseCommandMisspellText(c *C) {
	restore := advisor.ReplaceCommandsFinder(mkSillyFinder)
	defer restore()

	for _, misspelling := range []string{"helo", "0hello", "hell0", "hello0"} {
		err := snap.AdviseCommand(misspelling, "pretty")
		c.Assert(err, IsNil)
		c.Assert(s.Stdout(), Equals, fmt.Sprintf(`
Command "%s" not found, did you mean:

 command "hello" from snap "hello"
 command "hello" from snap "hello-wcm"

See 'snap info <snap name>' for additional versions.

`, misspelling))
		c.Assert(s.Stderr(), Equals, "")

		s.stdout.Reset()
		s.stderr.Reset()
	}
}
