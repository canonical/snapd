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

package main_test

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jessevdk/go-flags"
	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestHelpPrintsHelp(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, cmdLine := range [][]string{
		{"snap"},
		{"snap", "help"},
		{"snap", "--help"},
		{"snap", "-h"},
	} {
		s.ResetStdStreams()

		os.Args = cmdLine
		comment := check.Commentf("%q", cmdLine)

		err := snap.RunMain()
		c.Assert(err, check.IsNil, comment)
		c.Check(s.Stdout(), check.Matches, "(?s)"+strings.Join([]string{
			snap.LongSnapDescription,
			"",
			regexp.QuoteMeta(snap.SnapUsage),
			"", ".*", "",
			snap.SnapHelpAllFooter,
			snap.SnapHelpFooter,
		}, "\n")+`\s*`, comment)
		c.Check(s.Stderr(), check.Equals, "", comment)
	}
}

func (s *SnapSuite) TestHelpAllPrintsLongHelp(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"snap", "help", "--all"}

	err := snap.RunMain()
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, "(?sm)"+strings.Join([]string{
		snap.LongSnapDescription,
		"",
		regexp.QuoteMeta(snap.SnapUsage),
		"",
		snap.SnapHelpCategoriesIntro,
		"", ".*", "",
		snap.SnapHelpAllFooter,
	}, "\n")+`\s*`)
	c.Check(s.Stderr(), check.Equals, "")
}

func nonHiddenCommands() map[string]bool {
	parser := snap.Parser(snap.Client())
	commands := parser.Commands()
	names := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		if cmd.Hidden {
			continue
		}
		names[cmd.Name] = true
	}
	return names
}

func (s *SnapSuite) testSubCommandHelp(c *check.C, sub, expected string) {
	parser := snap.Parser(snap.Client())
	rest, err := parser.ParseArgs([]string{sub, "--help"})
	c.Assert(err, check.DeepEquals, &flags.Error{Type: flags.ErrHelp})
	c.Assert(rest, check.HasLen, 0)
	var buf bytes.Buffer
	parser.WriteHelp(&buf)
	c.Check(buf.String(), check.Equals, expected)
}

func (s *SnapSuite) TestSubCommandHelpPrintsHelp(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for cmd := range nonHiddenCommands() {
		s.ResetStdStreams()
		os.Args = []string{"snap", cmd, "--help"}

		err := snap.RunMain()
		comment := check.Commentf("%q", cmd)
		c.Assert(err, check.IsNil, comment)
		// regexp matches "Usage: snap <the command>" plus an arbitrary
		// number of [<something>] plus an arbitrary number of
		// <<something>> optionally ending in ellipsis
		c.Check(s.Stdout(), check.Matches, fmt.Sprintf(`(?sm)Usage:\s+snap %s(?: \[[^][]+\])*(?:(?: <[^<>]+>)+(?:\.\.\.)?)?$.*`, cmd), comment)
		c.Check(s.Stderr(), check.Equals, "", comment)
	}
}

func (s *SnapSuite) TestHelpCategories(c *check.C) {
	// non-hidden commands that are not expected to appear in the help summary
	excluded := []string{
		"help",
	}
	all := nonHiddenCommands()
	categorised := make(map[string]bool, len(all)+len(excluded))
	for _, cmd := range excluded {
		categorised[cmd] = true
	}
	seen := make(map[string]string, len(all))
	for _, categ := range snap.HelpCategories {
		for _, cmd := range categ.Commands {
			categorised[cmd] = true
			if seen[cmd] != "" {
				c.Errorf("duplicated: %q in %q and %q", cmd, seen[cmd], categ.Label)
			}
			seen[cmd] = categ.Label
		}
	}
	for cmd := range all {
		if !categorised[cmd] {
			c.Errorf("uncategorised: %q", cmd)
		}
	}
	for cmd := range categorised {
		if !all[cmd] {
			c.Errorf("unknown (hidden?): %q", cmd)
		}
	}
}
