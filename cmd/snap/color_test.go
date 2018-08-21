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
	"os"
	"runtime"
	// "fmt"
	// "net/http"

	"gopkg.in/check.v1"

	cmdsnap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snap"
)

func setEnviron(env map[string]string) func() {
	old := make(map[string]string, len(env))
	ok := make(map[string]bool, len(env))

	for k, v := range env {
		old[k], ok[k] = os.LookupEnv(k)
		if v != "" {
			os.Setenv(k, v)
		} else {
			os.Unsetenv(k)
		}
	}

	return func() {
		for k := range ok {
			if ok[k] {
				os.Setenv(k, old[k])
			} else {
				os.Unsetenv(k)
			}
		}
	}
}

func (s *SnapSuite) TestCanUnicode(c *check.C) {
	// setenv is per thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	type T struct {
		lang, lcAll, lcMsg string
		expected           bool
	}

	for _, t := range []T{
		{expected: false}, // all locale unset
		{lang: "C", expected: false},
		{lang: "C", lcAll: "C", expected: false},
		{lang: "C", lcAll: "C", lcMsg: "C", expected: false},
		{lang: "C.UTF-8", lcAll: "C", lcMsg: "C", expected: false}, // LC_MESSAGES wins
		{lang: "C.UTF-8", lcAll: "C.UTF-8", lcMsg: "C", expected: false},
		{lang: "C.UTF-8", lcAll: "C.UTF-8", lcMsg: "C.UTF-8", expected: true},
		{lang: "C.UTF-8", lcAll: "C", lcMsg: "C.UTF-8", expected: true},
		{lang: "C", lcAll: "C", lcMsg: "C.UTF-8", expected: true},
		{lang: "C", lcAll: "C.UTF-8", expected: true},
		{lang: "C.UTF-8", expected: true},
		{lang: "C.utf8", expected: true}, // deals with a bit of rando weirdness
	} {
		restore := setEnviron(map[string]string{"LANG": t.lang, "LC_ALL": t.lcAll, "LC_MESSAGES": t.lcMsg})
		c.Check(cmdsnap.CanUnicode("never"), check.Equals, false)
		c.Check(cmdsnap.CanUnicode("always"), check.Equals, true)
		c.Check(cmdsnap.CanUnicode("auto"), check.Equals, t.expected)
		restore()
	}
}

func (s *SnapSuite) TestColorTable(c *check.C) {
	// setenv is per thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	type T struct {
		isTTY         bool
		noColor, term string
		expected      interface{}
		desc          string
	}

	for _, t := range []T{
		{isTTY: false, expected: cmdsnap.NoEscColorTable, desc: "not a tty"},
		{isTTY: false, noColor: "1", expected: cmdsnap.NoEscColorTable, desc: "no tty *and* NO_COLOR set"},
		{isTTY: false, term: "linux-m", expected: cmdsnap.NoEscColorTable, desc: "no tty *and* mono term set"},
		{isTTY: true, expected: cmdsnap.ColorColorTable, desc: "is a tty"},
		{isTTY: true, noColor: "1", expected: cmdsnap.MonoColorTable, desc: "is a tty, but NO_COLOR set"},
		{isTTY: true, term: "linux-m", expected: cmdsnap.MonoColorTable, desc: "is a tty, but TERM=linux-m"},
		{isTTY: true, term: "xterm-mono", expected: cmdsnap.MonoColorTable, desc: "is a tty, but TERM=xterm-mono"},
	} {
		restoreIsTTY := cmdsnap.MockIsStdoutTTY(t.isTTY)
		restoreEnv := setEnviron(map[string]string{"NO_COLOR": t.noColor, "TERM": t.term})
		c.Check(cmdsnap.ColorTable("never"), check.DeepEquals, cmdsnap.NoEscColorTable, check.Commentf(t.desc))
		c.Check(cmdsnap.ColorTable("always"), check.DeepEquals, cmdsnap.ColorColorTable, check.Commentf(t.desc))
		c.Check(cmdsnap.ColorTable("auto"), check.DeepEquals, t.expected, check.Commentf(t.desc))
		restoreEnv()
		restoreIsTTY()
	}
}

func (s *SnapSuite) TestPublisherEscapes(c *check.C) {
	// just check never/always; for auto checks look above
	type T struct {
		color, unicode    bool
		username, display string
		verified          bool
		short, long, fill string
	}
	for _, t := range []T{
		// non-verified equal under fold:
		{color: false, unicode: false, username: "potato", display: "Potato",
			short: "potato", long: "Potato", fill: ""},
		{color: false, unicode: true, username: "potato", display: "Potato",
			short: "potato", long: "Potato", fill: ""},
		{color: true, unicode: false, username: "potato", display: "Potato",
			short: "potato\x1b[32m\x1b[0m", long: "Potato\x1b[32m\x1b[0m", fill: "\x1b[32m\x1b[0m"},
		{color: true, unicode: true, username: "potato", display: "Potato",
			short: "potato\x1b[32m\x1b[0m", long: "Potato\x1b[32m\x1b[0m", fill: "\x1b[32m\x1b[0m"},
		// verified equal under fold:
		{color: false, unicode: false, username: "potato", display: "Potato", verified: true,
			short: "potato*", long: "Potato*", fill: ""},
		{color: false, unicode: true, username: "potato", display: "Potato", verified: true,
			short: "potato✓", long: "Potato✓", fill: ""},
		{color: true, unicode: false, username: "potato", display: "Potato", verified: true,
			short: "potato\x1b[32m*\x1b[0m", long: "Potato\x1b[32m*\x1b[0m", fill: "\x1b[32m\x1b[0m"},
		{color: true, unicode: true, username: "potato", display: "Potato", verified: true,
			short: "potato\x1b[32m✓\x1b[0m", long: "Potato\x1b[32m✓\x1b[0m", fill: "\x1b[32m\x1b[0m"},
		// non-verified, different
		{color: false, unicode: false, username: "potato", display: "Carrot",
			short: "potato", long: "Carrot (potato)", fill: ""},
		{color: false, unicode: true, username: "potato", display: "Carrot",
			short: "potato", long: "Carrot (potato)", fill: ""},
		{color: true, unicode: false, username: "potato", display: "Carrot",
			short: "potato\x1b[32m\x1b[0m", long: "Carrot (potato\x1b[32m\x1b[0m)", fill: "\x1b[32m\x1b[0m"},
		{color: true, unicode: true, username: "potato", display: "Carrot",
			short: "potato\x1b[32m\x1b[0m", long: "Carrot (potato\x1b[32m\x1b[0m)", fill: "\x1b[32m\x1b[0m"},
		// verified, different
		{color: false, unicode: false, username: "potato", display: "Carrot", verified: true,
			short: "potato*", long: "Carrot (potato*)", fill: ""},
		{color: false, unicode: true, username: "potato", display: "Carrot", verified: true,
			short: "potato✓", long: "Carrot (potato✓)", fill: ""},
		{color: true, unicode: false, username: "potato", display: "Carrot", verified: true,
			short: "potato\x1b[32m*\x1b[0m", long: "Carrot (potato\x1b[32m*\x1b[0m)", fill: "\x1b[32m\x1b[0m"},
		{color: true, unicode: true, username: "potato", display: "Carrot", verified: true,
			short: "potato\x1b[32m✓\x1b[0m", long: "Carrot (potato\x1b[32m✓\x1b[0m)", fill: "\x1b[32m\x1b[0m"},
		// some interesting equal-under-folds:
		{color: false, unicode: false, username: "potato", display: "PoTaTo",
			short: "potato", long: "PoTaTo", fill: ""},
		{color: false, unicode: false, username: "potato-team", display: "Potato Team",
			short: "potato-team", long: "Potato Team", fill: ""},
	} {
		pub := &snap.StoreAccount{Username: t.username, DisplayName: t.display}
		if t.verified {
			pub.Validation = "verified"
		}
		color := "never"
		if t.color {
			color = "always"
		}
		unicode := "never"
		if t.unicode {
			unicode = "always"
		}

		mx := cmdsnap.ColorMixin(color, unicode)
		esc := cmdsnap.ColorMixinGetEscapes(mx)

		c.Check(cmdsnap.ShortPublisher(esc, pub), check.Equals, t.short)
		c.Check(cmdsnap.LongPublisher(esc, pub), check.Equals, t.long)
		c.Check(cmdsnap.FillerPublisher(esc), check.Equals, t.fill)
	}
}
