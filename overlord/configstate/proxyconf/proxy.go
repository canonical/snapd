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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

type ProxySettings struct {
	st *state.State
}

func New(st *state.State) *ProxySettings {
	return &ProxySettings{st: st}
}

func (p *ProxySettings) Conf(req *http.Request) (*url.URL, error) {
	p.st.Lock()
	tr := config.NewTransaction(p.st)
	p.st.Unlock()

	var proxy string
	mylog.Check(tr.Get("core", fmt.Sprintf("proxy.%s", req.URL.Scheme), &proxy))
	if proxy == "" || config.IsNoOption(err) {
		return http.ProxyFromEnvironment(req)
	}

	url := mylog.Check2(url.Parse(proxy))

	return url, nil
}
