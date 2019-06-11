// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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

package daemon

import (
	"net/http"
	"net/http/pprof"

	"github.com/gorilla/mux"

	"github.com/snapcore/snapd/overlord/auth"
)

var debugPprofCmd = &Command{
	PathPrefix: "/v2/debug/pprof/",
	GET:        getPprof,
	RootOnly:   true,
}

func getPprof(c *Command, r *http.Request, user *auth.UserState) Response {
	router := mux.NewRouter()
	router.HandleFunc("/v2/debug/pprof/cmdline", pprof.Cmdline)
	router.HandleFunc("/v2/debug/pprof/profile", pprof.Profile)
	router.HandleFunc("/v2/debug/pprof/symbol", pprof.Symbol)
	router.HandleFunc("/v2/debug/pprof/trace", pprof.Trace)
	for _, profile := range []string{"heap", "allocs", "block", "threadcreate", "goroutine", "mutex"} {
		router.Handle("/v2/debug/pprof/"+profile, pprof.Handler(profile))
	}
	return router
}
