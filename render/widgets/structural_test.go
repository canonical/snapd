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

package widgets_test

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/render"
	"github.com/snapcore/snapd/render/widgets"
)

type demoSuite struct {
	stdout *os.File
}

func (s *demoSuite) StdoutText() string {
	s.stdout.Seek(0, 0)
	blob, err := ioutil.ReadAll(s.stdout)
	if err != nil {
		panic(err)
	}
	return string(blob)
}

var _ = Suite(&demoSuite{})

func (s *demoSuite) SetUpTest(c *C) {
	f, err := ioutil.TempFile("", "stdout-*.txt")
	c.Assert(err, IsNil)
	s.stdout = f
}

func (s *demoSuite) TearDownTest(c *C) {
	if s.stdout != nil {
		defer s.stdout.Close()
		os.Remove(s.stdout.Name())
	}
}

func (s *demoSuite) TestRenderDemo(c *C) {
	render.Display(s.stdout, widgets.Seq(
		widgets.H1("Welcome to this rendering demo!"),
		widgets.T("This demo shows how various elements work together."),
		widgets.H2("This is an itemized list"),
		widgets.List("-", widgets.T("This is a list item\n"+"It spans multiple lines"),
			widgets.Seq(
				widgets.T("This is another item"),
				widgets.List("*", widgets.T("This is a nested list")))),
		widgets.H2("This is a key-value map"),
		widgets.Map(map[string]render.Widget{
			"Αα": widgets.T("The first letter of the Greek alphabet"),
			"Ωω": widgets.T("The last letter of the Greek alphabet"),
			// The wrong formatting created by gofmt is caused by gofmt's
			// unawareness of double-width characters. Oh the irony ;)
			"ひ": widgets.T("The HI syllable in Hiragana"),
		}),
		widgets.T("(keys in the map are always sorted)"),
	))
	c.Check(s.StdoutText(), Equals, ""+
		"\n"+
		"\n"+
		"# Welcome to this rendering demo!\n"+
		"\n"+
		"This demo shows how various elements work together.\n"+
		"\n"+
		"## This is an itemized list\n"+
		" - This is a list item\n"+
		"   It spans multiple lines\n"+
		" - This is another item\n"+
		"    * This is a nested list\n"+
		"\n"+
		"## This is a key-value map\n"+
		"   Αα: The first letter of the Greek alphabet\n"+
		"   Ωω: The last letter of the Greek alphabet\n"+
		"   ひ: The HI syllable in Hiragana\n"+
		"(keys in the map are always sorted)\n")
}
