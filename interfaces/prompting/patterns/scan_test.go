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

package patterns

import (
	. "gopkg.in/check.v1"
)

type scanSuite struct{}

var _ = Suite(&scanSuite{})

func (s *scanSuite) TestScanHappy(c *C) {
	pattern := "/{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld{-*,64}.so*"

	expectedTokens := []token{
		{tType: tokText, text: "/"},
		{tType: tokBraceOpen, text: "{"},
		{tType: tokComma, text: ","},
		{tType: tokText, text: "usr/"},
		{tType: tokBraceClose, text: "}"},
		{tType: tokText, text: "lib"},
		{tType: tokBraceOpen, text: "{"},
		{tType: tokComma, text: ","},
		{tType: tokText, text: "32"},
		{tType: tokComma, text: ","},
		{tType: tokText, text: "64"},
		{tType: tokComma, text: ","},
		{tType: tokText, text: "x32"},
		{tType: tokBraceClose, text: "}"},
		{tType: tokText, text: "/"},
		{tType: tokBraceOpen, text: "{"},
		{tType: tokComma, text: ","},
		{tType: tokText, text: "@"},
		{tType: tokBraceOpen, text: "{"},
		{tType: tokText, text: "multiarch"},
		{tType: tokBraceClose, text: "}"},
		{tType: tokText, text: "/"},
		{tType: tokBraceOpen, text: "{"},
		{tType: tokComma, text: ","},
		{tType: tokText, text: "atomics/"},
		{tType: tokBraceClose, text: "}"},
		{tType: tokBraceClose, text: "}"},
		{tType: tokText, text: "ld"},
		{tType: tokBraceOpen, text: "{"},
		{tType: tokText, text: "-*"},
		{tType: tokComma, text: ","},
		{tType: tokText, text: "64"},
		{tType: tokBraceClose, text: "}"},
		{tType: tokText, text: ".so*"},
	}

	tokens, err := scan(pattern)
	c.Check(err, IsNil)
	c.Check(tokens, DeepEquals, expectedTokens)

	patternWithEscapedMetachars := `/foo\{a\,b\,c\}\[bar\]\\`
	expectedTokens = []token{
		{tType: tokText, text: patternWithEscapedMetachars},
	}
	tokens, err = scan(patternWithEscapedMetachars)
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
		tokens, err := scan(testCase.pattern)
		c.Check(err, ErrorMatches, testCase.expectedErr)
		c.Check(tokens, IsNil)
	}
}
