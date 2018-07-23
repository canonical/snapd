// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"os"
	"os/signal"
	"syscall"

	"github.com/snapcore/snapd/tests/lib/fakestore/store"
)

type cmdRun struct {
	TopDir         string `long:"dir" description:"Directory to be used by the store to keep and serve snaps, <dir>/asserts is used for assertions"`
	Addr           string `long:"addr" default:"localhost:11028" description:"Store address"`
	AssertFallback bool   `long:"assert-fallback" description:"Fallback to the main online store for missing assertions"`

	HttpProxy  string `long:"http-proxy" description:"HTTP proxy address"`
	HttpsProxy string `long:"https-proxy" description:"HTTPS proxy address"`
}

var shortRunHelp = "Run the store service"

func (x *cmdRun) Execute(args []string) error {
	if x.HttpsProxy != "" {
		os.Setenv("https_proxy", x.HttpsProxy)
	}
	if x.HttpProxy != "" {
		os.Setenv("http_proxy", x.HttpProxy)
	}

	return runServer(x.TopDir, x.Addr, x.AssertFallback)
}

func init() {
	if _, err := parser.AddCommand("run", shortRunHelp, "", &cmdRun{}); err != nil {
		panic(err)
	}
}

func runServer(topDir, addr string, assertFallback bool) error {
	st := store.NewStore(topDir, addr, assertFallback)

	if err := st.Start(); err != nil {
		return err
	}

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch

	return st.Stop()
}
