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

package proxyconf

import (
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/net/http/httpproxy"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

type ProxySettings struct {
	st *state.State
}

func New(st *state.State) *ProxySettings {
	return &ProxySettings{st: st}
}

// Conf returns the proxy to use for the given request, or nil if the
// request should be made directly. It is suitable for use as
// http.Transport.Proxy.
//
// The core config options proxy.http, proxy.https and proxy.no-proxy
// take precedence over the respective environment variables
// (HTTP_PROXY, HTTPS_PROXY and NO_PROXY, or their lowercase variants),
// which are used for any option that is unset. As with
// http.ProxyFromEnvironment, requests to localhost and to loopback
// addresses are never proxied, and requests to hosts matching the
// no-proxy list are made directly.
func (p *ProxySettings) Conf(req *http.Request) (*url.URL, error) {
	cfg, err := p.proxyConfig()
	if err != nil {
		return nil, err
	}
	return cfg.ProxyFunc()(req.URL)
}

func (p *ProxySettings) proxyConfig() (*httpproxy.Config, error) {
	// the environment is read on each call (unlike
	// http.ProxyFromEnvironment which caches it), this is cheap
	cfg := httpproxy.FromEnvironment()

	p.st.Lock()
	tr := config.NewTransaction(p.st)
	p.st.Unlock()

	for _, opt := range []struct {
		key     string
		value   *string
		isProxy bool
	}{
		{"proxy.http", &cfg.HTTPProxy, true},
		{"proxy.https", &cfg.HTTPSProxy, true},
		{"proxy.no-proxy", &cfg.NoProxy, false},
	} {
		var value string
		if err := tr.Get("core", opt.key, &value); err != nil && !config.IsNoOption(err) {
			return nil, err
		}
		if value == "" {
			continue
		}
		// httpproxy silently ignores proxies it cannot parse, which
		// would make requests bypass a configured proxy
		if opt.isProxy && !isValidProxy(value) {
			// do not include the value, it may contain credentials
			return nil, fmt.Errorf("cannot use invalid core config option %q", opt.key)
		}
		*opt.value = value
		if opt.value == &cfg.HTTPProxy {
			// like http.ProxyFromEnvironment, httpproxy refuses
			// HTTP_PROXY in a CGI environment, where it may come
			// from a request header; that cannot be the case for
			// proxy.http
			cfg.CGI = false
		}
	}
	return cfg, nil
}

// isValidProxy mirrors the parsing done by httpproxy, which also
// accepts a proxy given without a scheme, as host:port.
func isValidProxy(proxy string) bool {
	u, err := url.Parse(proxy)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "socks5") {
		return true
	}
	_, err = url.Parse("http://" + proxy)
	return err == nil
}
