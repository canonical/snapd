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
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/proxyconf"
	"github.com/snapcore/snapd/overlord/state"
)

func TestT(t *testing.T) { TestingT(t) }

type proxyconfSuite struct {
	restoreEnv []func()
}

var _ = Suite(&proxyconfSuite{})

var proxyEnvVars = []string{
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"NO_PROXY", "no_proxy",
	"REQUEST_METHOD",
}

func (s *proxyconfSuite) SetUpTest(c *C) {
	// start from a clean environment, restoring it afterwards
	for _, name := range proxyEnvVars {
		s.setenv(c, name, "")
	}
}

func (s *proxyconfSuite) TearDownTest(c *C) {
	for i := len(s.restoreEnv) - 1; i >= 0; i-- {
		s.restoreEnv[i]()
	}
	s.restoreEnv = nil
}

func (s *proxyconfSuite) setenv(c *C, name, value string) {
	old, ok := os.LookupEnv(name)
	s.restoreEnv = append(s.restoreEnv, func() {
		if ok {
			os.Setenv(name, old)
		} else {
			os.Unsetenv(name)
		}
	})
	if value == "" {
		c.Assert(os.Unsetenv(name), IsNil)
	} else {
		c.Assert(os.Setenv(name, value), IsNil)
	}
}

func setCoreConfig(st *state.State, settings map[string]string) {
	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(st)
	for k, v := range settings {
		tr.Set("core", k, v)
	}
	tr.Commit()
}

func (s *proxyconfSuite) checkProxy(c *C, proxyConf *proxyconf.ProxySettings, reqURL, expected string) {
	req, err := http.NewRequest("GET", reqURL, nil)
	c.Assert(err, IsNil)
	proxy, err := proxyConf.Conf(req)
	c.Assert(err, IsNil)
	if expected == "" {
		c.Check(proxy, IsNil, Commentf("%s", reqURL))
	} else {
		c.Assert(proxy, NotNil, Commentf("%s", reqURL))
		c.Check(proxy.String(), Equals, expected, Commentf("%s", reqURL))
	}
}

func (s *proxyconfSuite) TestProxySettingsNoSetting(c *C) {
	st := state.New(nil)

	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Assert(err, IsNil)

	expected, err := http.ProxyFromEnvironment(req)
	c.Assert(err, IsNil)

	proxyConf := proxyconf.New(st)
	proxy, err := proxyConf.Conf(req)
	c.Assert(err, IsNil)
	c.Check(proxy, DeepEquals, expected)
}

func (s *proxyconfSuite) TestProxySettings(c *C) {
	st := state.New(nil)

	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Assert(err, IsNil)

	st.Lock()
	tr := config.NewTransaction(st)
	tr.Set("core", "proxy.http", "http://some-proxy:3128")
	tr.Commit()
	st.Unlock()

	proxyConf := proxyconf.New(st)
	proxy, err := proxyConf.Conf(req)
	c.Assert(err, IsNil)
	c.Check(proxy, DeepEquals, &url.URL{
		Scheme: "http",
		Host:   "some-proxy:3128",
	})
}

func (s *proxyconfSuite) TestProxySettingsHTTPS(c *C) {
	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		"proxy.http":  "http://http-proxy:3128",
		"proxy.https": "http://https-proxy:3128",
	})

	proxyConf := proxyconf.New(st)
	s.checkProxy(c, proxyConf, "http://example.com", "http://http-proxy:3128")
	s.checkProxy(c, proxyConf, "https://example.com", "http://https-proxy:3128")
}

func (s *proxyconfSuite) TestProxySettingsHTTPSProxyOverHTTPS(c *C) {
	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		"proxy.https": "https://user:pass@some-proxy:3129",
	})

	proxyConf := proxyconf.New(st)
	s.checkProxy(c, proxyConf, "https://example.com", "https://user:pass@some-proxy:3129")
	// proxy.https is not used for http requests
	s.checkProxy(c, proxyConf, "http://example.com", "")
}

func (s *proxyconfSuite) TestProxySettingsWithoutScheme(c *C) {
	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		"proxy.http": "some-proxy:3128",
	})

	proxyConf := proxyconf.New(st)
	s.checkProxy(c, proxyConf, "http://example.com", "http://some-proxy:3128")
}

func (s *proxyconfSuite) TestProxySettingsLoopbackIsDirect(c *C) {
	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		"proxy.http":  "http://some-proxy:3128",
		"proxy.https": "http://some-proxy:3128",
	})

	proxyConf := proxyconf.New(st)
	for _, u := range []string{
		"http://127.0.0.1:3142/v2/snaps/refresh",
		"http://127.1.2.3/",
		"https://127.0.0.1/",
		"http://localhost:11028/",
		"https://localhost/",
		"http://[::1]:3142/",
	} {
		s.checkProxy(c, proxyConf, u, "")
	}
	s.checkProxy(c, proxyConf, "http://example.com", "http://some-proxy:3128")
}

func (s *proxyconfSuite) TestProxySettingsNoProxy(c *C) {
	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		"proxy.http":     "http://some-proxy:3128",
		"proxy.https":    "http://some-proxy:3128",
		"proxy.no-proxy": "example.com,.internal,10.0.0.0/8,192.168.1.1:8080",
	})

	proxyConf := proxyconf.New(st)
	for _, u := range []string{
		"http://example.com/",
		"https://example.com/",
		"https://sub.example.com/",
		"http://store.internal/",
		"http://10.1.2.3:8000/",
		"http://192.168.1.1:8080/",
	} {
		s.checkProxy(c, proxyConf, u, "")
	}
	for _, u := range []string{
		"http://example.org/",
		"https://api.snapcraft.io/",
		"http://internal/",
		"http://192.168.1.1/",
		"http://11.0.0.1/",
	} {
		s.checkProxy(c, proxyConf, u, "http://some-proxy:3128")
	}
}

func (s *proxyconfSuite) TestProxySettingsNoProxyAppliesToEnvProxy(c *C) {
	s.setenv(c, "HTTP_PROXY", "http://env-proxy:3128")

	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		"proxy.no-proxy": "example.com",
	})

	proxyConf := proxyconf.New(st)
	s.checkProxy(c, proxyConf, "http://example.com", "")
	s.checkProxy(c, proxyConf, "http://example.org", "http://env-proxy:3128")
}

func (s *proxyconfSuite) TestProxySettingsEnvNoProxyWithConfigProxy(c *C) {
	s.setenv(c, "no_proxy", "example.com")

	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		"proxy.http": "http://some-proxy:3128",
	})

	proxyConf := proxyconf.New(st)
	s.checkProxy(c, proxyConf, "http://example.com", "")
	s.checkProxy(c, proxyConf, "http://example.org", "http://some-proxy:3128")
}

func (s *proxyconfSuite) TestProxySettingsConfigOverridesEnv(c *C) {
	s.setenv(c, "HTTP_PROXY", "http://env-proxy:3128")
	s.setenv(c, "HTTPS_PROXY", "http://env-proxy:3128")
	s.setenv(c, "NO_PROXY", "example.com")

	st := state.New(nil)
	proxyConf := proxyconf.New(st)
	// environment is used when nothing is configured
	s.checkProxy(c, proxyConf, "http://example.com", "")
	s.checkProxy(c, proxyConf, "http://example.org", "http://env-proxy:3128")
	s.checkProxy(c, proxyConf, "https://example.org", "http://env-proxy:3128")

	setCoreConfig(st, map[string]string{
		"proxy.http":     "http://some-proxy:3128",
		"proxy.no-proxy": "example.org",
	})
	// the configured options take precedence, each on its own
	s.checkProxy(c, proxyConf, "http://example.com", "http://some-proxy:3128")
	s.checkProxy(c, proxyConf, "http://example.org", "")
	s.checkProxy(c, proxyConf, "https://example.com", "http://env-proxy:3128")
	s.checkProxy(c, proxyConf, "https://example.org", "")

	// and changes apply without a restart
	setCoreConfig(st, map[string]string{
		"proxy.http":     "",
		"proxy.no-proxy": "",
	})
	s.checkProxy(c, proxyConf, "http://example.com", "")
	s.checkProxy(c, proxyConf, "http://example.org", "http://env-proxy:3128")
}

func (s *proxyconfSuite) TestProxySettingsInvalidProxy(c *C) {
	st := state.New(nil)
	setCoreConfig(st, map[string]string{
		// not valid even with http:// prepended, which httpproxy
		// would silently ignore
		"proxy.http": "http://user:secret@some-proxy:3128/%zz",
	})

	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Assert(err, IsNil)

	proxyConf := proxyconf.New(st)
	_, err = proxyConf.Conf(req)
	c.Check(err, ErrorMatches, `cannot use invalid core config option "proxy.http"`)
	c.Check(err.Error(), Not(Matches), `.*secret.*`)
}

func (s *proxyconfSuite) TestProxySettingsCGI(c *C) {
	s.setenv(c, "REQUEST_METHOD", "GET")
	s.setenv(c, "HTTP_PROXY", "http://env-proxy:3128")

	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Assert(err, IsNil)

	st := state.New(nil)
	proxyConf := proxyconf.New(st)
	// same as http.ProxyFromEnvironment
	_, err = proxyConf.Conf(req)
	c.Check(err, ErrorMatches, `refusing to use HTTP_PROXY value in CGI environment.*`)

	// but proxy.http is not affected
	setCoreConfig(st, map[string]string{
		"proxy.http": "http://some-proxy:3128",
	})
	s.checkProxy(c, proxyConf, "http://example.com", "http://some-proxy:3128")
}
