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
	"bytes"
	"fmt"
	"time"

	"github.com/ubuntu-core/snappy/strutil"
)

// Token contains the sso token
type Token struct {
	TokenKey       string `json:"token_key"`
	TokenSecret    string `json:"token_secret"`
	ConsumerSecret string `json:"consumer_secret"`
	ConsumerKey    string `json:"consumer_key"`
}

// see https://dev.twitter.com/oauth/overview/percent-encoding-parameters
func needsEscape(c byte) bool {
	return !(('A' <= c && c <= 'Z') ||
		('a' <= c && c <= 'z') ||
		('0' <= c && c <= '9') ||
		(c == '-') ||
		(c == '.') ||
		(c == '_') ||
		(c == '~'))
}

// quote will quote all bytes in the input string that oauth requries to
// be quoted
func quote(s string) string {
	buf := bytes.NewBuffer(nil)
	// set to worst case max size, to avoid reallocs
	sin := []byte(s)
	buf.Grow(len(sin) * 3)

	for _, c := range sin {
		if needsEscape(c) {
			fmt.Fprintf(buf, "%%%02X", c)
		} else {
			fmt.Fprintf(buf, "%c", c)
		}
	}

	return buf.String()
}

// FIXME: replace with a real oauth1 library - or wait until oauth2 becomes
// available

// MakePlaintextSignature makes a oauth v1 plaintext signature
func MakePlaintextSignature(token *Token) string {
	// hrm, rfc5849 says that nonce, timestamp are not used for PLAINTEXT
	// but our sso server is unhappy without, so
	nonce := strutil.MakeRandomString(60)
	timestamp := time.Now().Unix()

	s := fmt.Sprintf(`OAuth oauth_nonce="%s", oauth_timestamp="%v", oauth_version="1.0", oauth_signature_method="PLAINTEXT", oauth_consumer_key="%s", oauth_token="%s", oauth_signature="%s%%26%s"`, nonce, timestamp, quote(token.ConsumerKey), quote(token.TokenKey), quote(token.ConsumerSecret), quote(token.TokenSecret))
	return s
}
