// +build !go1.8

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package httputil

import (
	"net/http"
)

func fixupHeadersForRedirect(req *http.Request, via []*http.Request) {
	// preserve some headers across redirects
	// to the CDN
	// (this is done automatically, slightly more cleanly, from 1.8)
	for k, v := range via[0].Header {
		switch http.CanonicalHeaderKey(k) {
		case "Authorization", "Www-Authenticate", "Cookie", "Cookie2":
			// whistle innocently
		default:
			req.Header[k] = v
		}
	}
}
