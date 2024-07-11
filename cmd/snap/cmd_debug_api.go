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
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/logger"
)

var longDebugAPIHelp = `
Execute a raw query to snapd API. Complex input can be read from stdin, while
output is printed to stdout. See examples below:

List all snaps:
$ snap debug api /v2/snaps

Find snaps with name foo:
$ snap debug api '/v2/find?name=foo'

Request refresh of snap 'some-snap':
$ echo '{"action": "refresh"}' | snap debug api -X POST \
      -H 'Content-Type: application/json' /v2/snaps/some-snap
`

type cmdDebugAPI struct {
	clientMixin

	Headers []string `short:"H" long:"header"`
	Method  string   `short:"X" long:"request"`
	Fail    bool     `long:"fail"`
	// TODO support -F/--form like curl
	// TODO support -d/--data like curl

	Positional struct {
		PathAndQuery string `positional-arg-name:"<path-and-query>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addDebugCommand("api",
		"Execute raw query to snapd API",
		longDebugAPIHelp,
		func() flags.Commander {
			return &cmdDebugAPI{}
		}, map[string]string{
			"header":  "Set header (can be repeated multiple times), header kind and value are separated with ': '",
			"request": "HTTP method to use (defaults to GET)",
			"fail":    "Fail on request errors",
		}, nil)
}

func (x *cmdDebugAPI) Execute(args []string) error {
	method := x.Method
	switch method {
	case "GET", "POST", "PUT":
	case "":
		method = "GET"
	default:
		return fmt.Errorf("unsupported method %q", method)
	}

	var in io.Reader
	switch method {
	case "POST", "PUT":
		in = Stdin
	}

	u, err := url.Parse(x.Positional.PathAndQuery)
	if err != nil {
		return err
	}

	hdrs := x.Headers
	reqHdrs := make(map[string]string, len(hdrs))
	for _, hdr := range x.Headers {
		before, after, sep := strings.Cut(hdr, ": ")
		if !sep {
			return fmt.Errorf("cannot parse header %q", hdr)
		}
		reqHdrs[before] = after
	}
	logger.Debugf("url: %v", u.Path)
	logger.Debugf("query: %v", u.RawQuery)
	logger.Debugf("headers: %s", reqHdrs)

	rsp, err := x.client.DebugRaw(context.Background(), method, u.Path, u.Query(), reqHdrs, in)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()

	logger.Debugf("response: %v", rsp.Status)
	logger.Debugf("response headers: %v", rsp.Header)
	logger.Debugf("response length: %v", rsp.ContentLength)

	if rsp.Header.Get("Content-Type") == "application/json" {
		// pretty print JSON response by default

		var temp map[string]interface{}
		if err := json.NewDecoder(rsp.Body).Decode(&temp); err != nil {
			return err
		}
		enc := json.NewEncoder(Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(temp); err != nil {
			return err
		}
	} else {
		if _, err := io.Copy(Stdout, rsp.Body); err != nil {
			return err
		}
	}

	if x.Fail && rsp.StatusCode >= 400 {
		// caller wants to fail on non success requests
		return fmt.Errorf("request failed with status %v", rsp.Status)
	}

	return nil
}
