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

package httputil_test

import (
	"net/http"
	"net/url"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/httputil"
)

type clientSuite struct{}

var _ = check.Suite(&clientSuite{})

func mustParse(c *check.C, rawurl string) *url.URL {
	url, err := url.Parse("http://some-proxy:3128")
	c.Assert(err, check.IsNil)
	return url
}

type proxyProvider struct {
	proxy *url.URL
}

func (p *proxyProvider) proxyCallback(*http.Request) (*url.URL, error) {
	return p.proxy, nil
}

func (s *clientSuite) TestClientOptionsWithProxy(c *check.C) {
	pp := proxyProvider{proxy: mustParse(c, "http://some-proxy:3128")}
	cli := httputil.NewHTTPClient(&httputil.ClientOptions{
		Proxy: pp.proxyCallback,
	})
	c.Assert(cli, check.NotNil)

	trans := cli.Transport.(*httputil.LoggedTransport).Transport.(*http.Transport)
	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Check(err, check.IsNil)
	url, err := trans.Proxy(req)
	c.Check(err, check.IsNil)
	c.Check(url.String(), check.Equals, "http://some-proxy:3128")
}
