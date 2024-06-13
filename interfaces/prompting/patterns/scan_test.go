// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
 * along with this program.  If not, Text: see <http://www.gnu.org/licenses/>.
 *
 */

package patterns_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting/patterns"
)

type scanSuite struct{}

var _ = Suite(&scanSuite{})

func (s *scanSuite) TestScanHappy(c *C) {
	pattern := "/{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld{-*,64}.so*"

	expectedTokens := []patterns.Token{
		patterns.Token{Type: patterns.TokText, Text: "/"},
		patterns.Token{Type: patterns.TokBraceOpen, Text: "{"},
		patterns.Token{Type: patterns.TokComma, Text: ","},
		patterns.Token{Type: patterns.TokText, Text: "usr/"},
		patterns.Token{Type: patterns.TokBraceClose, Text: "}"},
		patterns.Token{Type: patterns.TokText, Text: "lib"},
		patterns.Token{Type: patterns.TokBraceOpen, Text: "{"},
		patterns.Token{Type: patterns.TokComma, Text: ","},
		patterns.Token{Type: patterns.TokText, Text: "32"},
		patterns.Token{Type: patterns.TokComma, Text: ","},
		patterns.Token{Type: patterns.TokText, Text: "64"},
		patterns.Token{Type: patterns.TokComma, Text: ","},
		patterns.Token{Type: patterns.TokText, Text: "x32"},
		patterns.Token{Type: patterns.TokBraceClose, Text: "}"},
		patterns.Token{Type: patterns.TokText, Text: "/"},
		patterns.Token{Type: patterns.TokBraceOpen, Text: "{"},
		patterns.Token{Type: patterns.TokComma, Text: ","},
		patterns.Token{Type: patterns.TokText, Text: "@"},
		patterns.Token{Type: patterns.TokBraceOpen, Text: "{"},
		patterns.Token{Type: patterns.TokText, Text: "multiarch"},
		patterns.Token{Type: patterns.TokBraceClose, Text: "}"},
		patterns.Token{Type: patterns.TokText, Text: "/"},
		patterns.Token{Type: patterns.TokBraceOpen, Text: "{"},
		patterns.Token{Type: patterns.TokComma, Text: ","},
		patterns.Token{Type: patterns.TokText, Text: "atomics/"},
		patterns.Token{Type: patterns.TokBraceClose, Text: "}"},
		patterns.Token{Type: patterns.TokBraceClose, Text: "}"},
		patterns.Token{Type: patterns.TokText, Text: "ld"},
		patterns.Token{Type: patterns.TokBraceOpen, Text: "{"},
		patterns.Token{Type: patterns.TokText, Text: "-*"},
		patterns.Token{Type: patterns.TokComma, Text: ","},
		patterns.Token{Type: patterns.TokText, Text: "64"},
		patterns.Token{Type: patterns.TokBraceClose, Text: "}"},
		patterns.Token{Type: patterns.TokText, Text: ".so*"},
	}

	tokens, err := patterns.Scan(pattern)
	c.Check(err, IsNil)
	c.Check(tokens, DeepEquals, expectedTokens)

	patternWithEscapedMetachars := `/foo\{a\,b\,c\}\[bar\]\\`
	expectedTokens = []patterns.Token{
		patterns.Token{Type: patterns.TokText, Text: patternWithEscapedMetachars},
	}
	tokens, err = patterns.Scan(patternWithEscapedMetachars)
	c.Check(err, IsNil)
	c.Check(tokens, DeepEquals, expectedTokens)
}

func (s *scanSuite) TestScanUnhappy(c *C) {
	for _, testCase := range []struct {
		pattern     string
		expectedErr string
	}{
		{
			`/foo\`,
			`trailing unescaped '\\' character`,
		},
		{
			`/foo[bar`,
			`cannot contain unescaped '\[' or '\]' character`,
		},
		{
			`/foo]bar`,
			`cannot contain unescaped '\[' or '\]' character`,
		},
	} {
		tokens, err := patterns.Scan(testCase.pattern)
		c.Check(err, ErrorMatches, testCase.expectedErr)
		c.Check(tokens, IsNil)
	}
}
