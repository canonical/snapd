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
	"reflect"
	"regexp"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
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
		{"snap", "--help", "install"},
	} {
		s.ResetStdStreams()

		os.Args = cmdLine
		comment := check.Commentf("%q", cmdLine)
		mylog.Check(snap.RunMain())
		c.Assert(err, check.IsNil, comment)
		c.Check(s.Stdout(), check.Matches, "(?s)"+strings.Join([]string{
			snap.LongSnapDescription,
			"",
			regexp.QuoteMeta(snap.SnapUsage),
			"",
			snap.SnapHelpCategoriesIntro,
			".*", "",
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
	mylog.Check(snap.RunMain())
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, "(?sm)"+strings.Join([]string{
		snap.LongSnapDescription,
		"",
		regexp.QuoteMeta(snap.SnapUsage),
		"",
		snap.SnapHelpAllIntro,
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

// Helper that checks if goflags is old. The check for EnvNamespace is
// arbitrary, it just happened that support for this got added right after
// the v1.4.0 release with commit 1c38ed7.
func goFlagsFromBefore20200331() bool {
	v := reflect.ValueOf(flags.Group{})
	f := v.FieldByName("EnvNamespace")
	return !f.IsValid()
}

func (s *SnapSuite) testSubCommandHelp(c *check.C, sub, expected string) {
	// Skip --help output tests for older versions of
	// go-flags. Notably v1.4.0 from debian-sid will fail because
	// the formating is slightly different. Note that the check here
	// is not precise i.e. this is not the commit that added the change
	// that changed the help output but this change is easy to test for
	// with reflect and in practice this is fine.
	if goFlagsFromBefore20200331() {
		c.Skip("go flags too old")
	}

	parser := snap.Parser(snap.Client())
	rest := mylog.Check2(parser.ParseArgs([]string{sub, "--help"}))
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
		mylog.Check(snap.RunMain())
		comment := check.Commentf("%q", cmd)
		c.Assert(err, check.IsNil, comment)
		// regexp matches "Usage: snap <the command>" plus an arbitrary
		// number of [<something>] plus an arbitrary number of
		// <<something>> optionally ending in ellipsis
		c.Check(s.Stdout(), check.Matches, fmt.Sprintf(`(?sm)Usage:\s+snap %s(?: \[[^][]+\])*(?:(?: <[^<>]+>(?:[.:]<[^<>]+>)?)+(?:\.\.\.)?)?(?: \[[^][]+\])?$.*`, cmd), comment)
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
	seenCmds := func(cmds []string, label string) {
		for _, cmd := range cmds {
			categorised[cmd] = true
			if seen[cmd] != "" {
				c.Errorf("duplicated: %q in %q and %q", cmd, seen[cmd], label)
			}
			seen[cmd] = label
		}
	}
	for _, categ := range snap.HelpCategories {
		seenCmds(categ.Commands, categ.Label)
		seenCmds(categ.AllOnlyCommands, categ.Label)
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

func (s *SnapSuite) TestHelpCommandAllFails(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap", "help", "interfaces", "--all"}
	mylog.Check(snap.RunMain())
	c.Assert(err, check.ErrorMatches, "help accepts a command, or '--all', but not both.")
}

func (s *SnapSuite) TestManpageInSection8(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap", "help", "--man"}
	mylog.Check(snap.RunMain())
	c.Assert(err, check.IsNil)

	c.Check(s.Stdout(), check.Matches, `\.TH snap 8 (?s).*`)
}

func (s *SnapSuite) TestManpageNoDoubleTP(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap", "help", "--man"}
	mylog.Check(snap.RunMain())
	c.Assert(err, check.IsNil)

	c.Check(s.Stdout(), check.Not(check.Matches), `(?s).*(?m-s)^\.TP\n\.TP$(?s-m).*`)
}

func (s *SnapSuite) TestBadSub(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap", "debug", "brotato"}
	mylog.Check(snap.RunMain())
	c.Assert(err, check.ErrorMatches, `unknown command "brotato", see 'snap help debug'.`)
}
