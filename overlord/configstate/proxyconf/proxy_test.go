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

package proxyconf_test

import (
	"net/http"
	"net/url"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/proxyconf"
	"github.com/snapcore/snapd/overlord/state"
)

func TestT(t *testing.T) { TestingT(t) }

type proxyconfSuite struct{}

var _ = Suite(&proxyconfSuite{})

func (s *proxyconfSuite) TestProxySettingsNoSetting(c *C) {
	st := state.New(nil)

	req := mylog.Check2(http.NewRequest("GET", "http://example.com", nil))


	expected := mylog.Check2(http.ProxyFromEnvironment(req))


	proxyConf := proxyconf.New(st)
	proxy := mylog.Check2(proxyConf.Conf(req))

	c.Check(proxy, DeepEquals, expected)
}

func (s *proxyconfSuite) TestProxySettings(c *C) {
	st := state.New(nil)

	req := mylog.Check2(http.NewRequest("GET", "http://example.com", nil))


	st.Lock()
	tr := config.NewTransaction(st)
	tr.Set("core", "proxy.http", "http://some-proxy:3128")
	tr.Commit()
	st.Unlock()

	proxyConf := proxyconf.New(st)
	proxy := mylog.Check2(proxyConf.Conf(req))

	c.Check(proxy, DeepEquals, &url.URL{
		Scheme: "http",
		Host:   "some-proxy:3128",
	})
}
