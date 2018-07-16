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

package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
)

type colorMixin struct {
	Color   string `long:"color" default:"auto" choice:"auto" choice:"never" choice:"always"`
	Unicode string `long:"unicode" default:"auto" choice:"auto" choice:"never" choice:"always"` // do we want this hidden?
}

func (mx colorMixin) getEscapes() *escapes {
	esc := colorTable(mx.Color)
	if canUnicode(mx.Unicode) {
		esc.dash = "–" // that's an en dash (so yaml is happy)
		esc.uparrow = "↑"
		esc.tick = "✓"
	} else {
		esc.dash = "--" // two dashes keeps yaml happy also
		esc.uparrow = "^"
		esc.tick = "*"
	}

	return &esc
}

func canUnicode(mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	var lang string
	for _, k := range []string{"LC_MESSAGES", "LC_ALL", "LANG"} {
		lang = os.Getenv(k)
		if lang != "" {
			break
		}
	}
	if lang == "" {
		return false
	}
	lang = strings.ToUpper(lang)
	return strings.Contains(lang, "UTF-8") || strings.Contains(lang, "UTF8")
}

// TODO: maybe unify isTTY (~3 calls just in cmd/snap) (but note stdout vs stdin)
var isTTY = terminal.IsTerminal(1)

func colorTable(mode string) escapes {
	switch mode {
	case "always":
		return color
	case "never":
		return noesc
	}
	if !isTTY {
		return noesc
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		// from http://no-color.org/:
		//   command-line software which outputs text with ANSI color added should
		//   check for the presence of a NO_COLOR environment variable that, when
		//   present (regardless of its value), prevents the addition of ANSI color.
		return mono // bold & dim is still ok
	}
	if term := os.Getenv("TERM"); term == "xterm-mono" || term == "linux-m" {
		// these are often used to flag "I don't want to see color" more than "I can't do color"
		// (if you can't *do* color, `color` and `mono` should produce the same results)
		return mono
	}
	return color
}

var colorDescs = mixinDescs{
	"color":   i18n.G("Use a little bit of color to highlight some things."),
	"unicode": i18n.G("Use a little bit of Unicode to improve legibility."),
}

type escapes struct {
	green string
	end   string

	tick, dash, uparrow string
}

var (
	color = escapes{
		green: "\033[32m",
		end:   "\033[0m",
	}

	mono = escapes{
		green: "\033[1m",
		end:   "\033[0m",
	}

	noesc = escapes{}
)

// fillerPublisher is used to add an no-op escape sequence to a header in a
// tabwriter table, so that things line up.
func fillerPublisher(esc *escapes) string {
	return esc.green + esc.end
}

// longPublisher returns a string that'll present the publisher of a snap to the
// terminal user:
//
// * if the publisher's username and display name match, it's just the display
//   name; otherwise, it'll include the username in parentheses
//
// * if the publisher is verified, it'll include a green check mark; otherwise,
//   it'll include a no-op escape sequence of the same length as the escape
//   sequence used to make it green (this so that tabwriter gets things right).
func longPublisher(esc *escapes, storeAccount *snap.StoreAccount) string {
	if storeAccount == nil {
		return esc.dash + esc.green + esc.end
	}
	badge := ""
	if storeAccount.Validation == "verified" {
		badge = esc.tick
	}
	// NOTE this makes e.g. 'Potato' == 'potato', and 'Potato Team' == 'potato-team',
	// but 'Potato Team' != 'potatoteam', 'Potato Inc.' != 'potato' (in fact 'Potato Inc.' != 'potato-inc')
	if strings.EqualFold(strings.Replace(storeAccount.Username, "-", " ", -1), storeAccount.DisplayName) {
		return storeAccount.DisplayName + esc.green + badge + esc.end
	}
	return fmt.Sprintf("%s (%s%s%s%s)", storeAccount.DisplayName, storeAccount.Username, esc.green, badge, esc.end)
}

// longPublisher returns a string that'll present the publisher of a snap to the
// terminal user:
//
// * it'll always be just the username
//
// * if the publisher is verified, it'll include a green check mark; otherwise,
//   it'll include a no-op escape sequence of the same length as the escape
//   sequence used to make it green (this so that tabwriter gets things right).
func shortPublisher(esc *escapes, storeAccount *snap.StoreAccount) string {
	if storeAccount == nil {
		return "-" + esc.green + esc.end
	}
	badge := ""
	if storeAccount.Validation == "verified" {
		badge = esc.tick
	}
	return storeAccount.Username + esc.green + badge + esc.end

}
