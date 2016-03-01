// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package oauth

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type OAuthTestSuite struct{}

var _ = Suite(&OAuthTestSuite{})

func (s *OAuthTestSuite) TestMakePlaintextSignature(c *C) {
	mockToken := Token{
		ConsumerKey:    "consumer-key+",
		ConsumerSecret: "consumer-secret+",
		TokenKey:       "token-key+",
		TokenSecret:    "token-secret+",
	}
	sig := MakePlaintextSignature(&mockToken)
	c.Assert(sig, Matches, `OAuth oauth_nonce="[a-zA-Z0-9]+", oauth_timestamp="[0-9]+", oauth_version="1.0", oauth_signature_method="PLAINTEXT", oauth_consumer_key="consumer-key%2B", oauth_token="token-key%2B", oauth_signature="consumer-secret%2B%26token-secret%2B"`)
}

func (s *OAuthTestSuite) TestQuote(c *C) {
	// see http://wiki.oauth.net/w/page/12238556/TestCases
	c.Check(quote("abcABC123"), Equals, "abcABC123")
	c.Check(quote("-._~"), Equals, "-._~")
	c.Check(quote("%"), Equals, "%25")
	c.Check(quote("+"), Equals, "%2B")
	c.Check(quote("&=*"), Equals, "%26%3D%2A")
	c.Check(quote("\u000A"), Equals, "%0A")
	c.Check(quote("\u0020"), Equals, "%20")
	c.Check(quote("\u007F"), Equals, "%7F")
	c.Check(quote("\u0080"), Equals, "%C2%80")
	c.Check(quote("\u3001"), Equals, "%E3%80%81")
}

func (s *OAuthTestSuite) TestNeedsEscape(c *C) {
	for _, needed := range []byte{'?', '/', ':'} {
		c.Check(needsEscape(needed), Equals, true)
	}
}

func (s *OAuthTestSuite) TestNeedsNoEscape(c *C) {
	for _, no := range []byte{'a', 'z', 'A', 'Z', '-', '.', '_', '~'} {
		c.Check(needsEscape(no), Equals, false)
	}
}
