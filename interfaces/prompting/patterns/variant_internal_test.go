// -*- Mode: Go; indent-tabs-mode: t -*-

/*",
 * Copyright (C) 2024 Canonical Ltd
 *",
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *",
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *",
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, Text: see <http://www.gnu.org/licenses/>.
 *",
 */

package patterns

import (
	. "gopkg.in/check.v1"
)

type variantSuite struct{}

var _ = Suite(&variantSuite{})

func (s *variantSuite) TestParsePatternVariant(c *C) {
	for _, testCase := range []struct {
		pattern         string
		preparedPattern string
		components      []component
		variantStr      string
	}{
		{
			"/foo/bar/baz",
			"/foo/bar/baz",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: "bar"},
				{compType: compSeparator},
				{compType: compLiteral, compText: "baz"},
				{compType: compTerminal},
			},
			"/foo/bar/baz",
		},
		{
			"/foo/bar/baz/",
			"/foo/bar/baz/",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: "bar"},
				{compType: compSeparator},
				{compType: compLiteral, compText: "baz"},
				{compType: compSeparator},
				{compType: compTerminal},
			},
			"/foo/bar/baz/",
		},
		{
			"/?o*/b?r/*a?/",
			"/?o*/b?r/*a?/",
			[]component{
				{compType: compSeparator},
				{compType: compAnySingle},
				{compType: compLiteral, compText: "o"},
				{compType: compGlobstar},
				{compType: compSeparator},
				{compType: compLiteral, compText: "b"},
				{compType: compAnySingle},
				{compType: compLiteral, compText: "r"},
				{compType: compSeparator},
				{compType: compGlobstar},
				{compType: compLiteral, compText: "a"},
				{compType: compAnySingle},
				{compType: compSeparator},
				{compType: compTerminal},
			},
			"/?o*/b?r/*a?/",
		},
		{
			"/foo////bar",
			"/foo////bar",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: "bar"},
				{compType: compTerminal},
			},
			"/foo/bar",
		},
		{
			"/foo**/bar",
			"/foo⁑/bar",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compGlobstar},
				{compType: compSeparator},
				{compType: compLiteral, compText: "bar"},
				{compType: compTerminal},
			},
			"/foo*/bar",
		},
		{
			"/foo/**bar",
			"/foo/⁑bar",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compGlobstar},
				{compType: compLiteral, compText: "bar"},
				{compType: compTerminal},
			},
			"/foo/*bar",
		},
		{
			"/foo/**/**/bar",
			"/foo/⁑/⁑/bar",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparatorDoublestar},
				{compType: compSeparator},
				{compType: compLiteral, compText: "bar"},
				{compType: compTerminal},
			},
			"/foo/**/bar",
		},
		{
			"/foo/**/*/bar",
			"/foo/⁑/*/bar",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compGlobstar},
				{compType: compSeparatorDoublestar},
				{compType: compSeparator},
				{compType: compLiteral, compText: "bar"},
				{compType: compTerminal},
			},
			"/foo/*/**/bar",
		},
		{
			"/foo/**/**/",
			"/foo/⁑/⁑/",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparatorDoublestarSeparatorTerminal},
			},
			"/foo/**/",
		},
		{
			"/foo/**/**",
			"/foo/⁑/⁑",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparatorDoublestarTerminal},
			},
			"/foo/**",
		},
		{
			"/foo/**/*",
			"/foo/⁑/*",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparatorDoublestarTerminal},
			},
			"/foo/**",
		},
		{
			"/foo/**?/*?*?*",
			"/foo/⁑?/*?*?*",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compAnySingle},
				{compType: compGlobstar},
				{compType: compSeparator},
				{compType: compAnySingle},
				{compType: compAnySingle},
				{compType: compGlobstar},
				{compType: compTerminal},
			},
			"/foo/?*/??*",
		},
		{
			"/foo/**?/***?***?***",
			"/foo/⁑?/⁑*?⁑*?⁑*",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compAnySingle},
				{compType: compGlobstar},
				{compType: compSeparator},
				{compType: compAnySingle},
				{compType: compAnySingle},
				{compType: compGlobstar},
				{compType: compTerminal},
			},
			"/foo/?*/??*",
		},
		// Check that unicode in patterns treated as a single rune, and that escape
		// characters are not counted, even when escaping unicode runes.
		{
			"/foo/🚵🚵",
			"/foo/🚵🚵",
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: "🚵🚵"},
				{compType: compTerminal},
			},
			"/foo/🚵🚵",
		},
		{
			`/foo/\🚵\🚵\🚵\🚵\🚵`,
			`/foo/\🚵\🚵\🚵\🚵\🚵`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: `🚵🚵🚵🚵🚵`},
				{compType: compTerminal},
			},
			`/foo/🚵🚵🚵🚵🚵`,
		},
		{
			`/foo/\\`,
			`/foo/\\`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: `\\`},
				{compType: compTerminal},
			},
			`/foo/\\`,
		},
		{
			`/foo/⁑⁑⁑⁑⁑`,
			`/foo/\⁑\⁑\⁑\⁑\⁑`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: `⁑⁑⁑⁑⁑`},
				{compType: compTerminal},
			},
			`/foo/⁑⁑⁑⁑⁑`,
		},
		{
			`/foo/⁑\\⁑\\⁑\\⁑\\⁑`,
			`/foo/\⁑\\\⁑\\\⁑\\\⁑\\\⁑`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: `⁑\\⁑\\⁑\\⁑\\⁑`},
				{compType: compTerminal},
			},
			`/foo/⁑\\⁑\\⁑\\⁑\\⁑`,
		},
		{
			`/foo/⁑\⁑\\⁑\\\⁑\\\\⁑`,
			`/foo/\⁑\⁑\\\⁑\\\⁑\\\\\⁑`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: `⁑⁑\\⁑\\⁑\\\\⁑`},
				{compType: compTerminal},
			},
			`/foo/⁑⁑\\⁑\\⁑\\\\⁑`,
		},
		{
			`/foo/**********`,
			`/foo/⁑⁑⁑⁑⁑`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparatorDoublestarTerminal},
			},
			`/foo/**`,
		},
		{
			`/foo/\**\**\**\**\**`,
			`/foo/\**\**\**\**\**`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: `\*`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\*`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\*`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\*`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\*`},
				{compType: compGlobstar},
				{compType: compTerminal},
			},
			`/foo/\**\**\**\**\**`,
		},
		{
			`/foo/**\\**\\**\\**\\**`,
			`/foo/⁑\\⁑\\⁑\\⁑\\⁑`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\\`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\\`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\\`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\\`},
				{compType: compGlobstar},
				{compType: compTerminal},
			},
			`/foo/*\\*\\*\\*\\*`,
		},
		{
			`/foo/⁑**⁑\**\⁑*\\*⁑\\\**\\\⁑`,
			`/foo/\⁑⁑\⁑\**\⁑*\\*\⁑\\\**\\\⁑`,
			[]component{
				{compType: compSeparator},
				{compType: compLiteral, compText: "foo"},
				{compType: compSeparator},
				{compType: compLiteral, compText: `⁑`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `⁑\*`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `⁑`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\\`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `⁑\\\*`},
				{compType: compGlobstar},
				{compType: compLiteral, compText: `\\⁑`},
				{compType: compTerminal},
			},
			`/foo/⁑*⁑\**⁑*\\*⁑\\\**\\⁑`,
		},
	} {
		c.Check(prepareVariantForParsing(testCase.pattern), Equals, testCase.preparedPattern, Commentf("testCase: %+v", testCase))
		variant, err := ParsePatternVariant(testCase.pattern)
		c.Assert(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(variant.components, DeepEquals, testCase.components, Commentf("testCase: %+v", testCase))
		c.Check(variant.String(), DeepEquals, testCase.variantStr, Commentf("testCase: %+v", testCase))
	}
}
