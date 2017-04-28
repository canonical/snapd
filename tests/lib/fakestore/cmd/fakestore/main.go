// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/tests/lib/fakestore/refresh"
	"github.com/snapcore/snapd/tests/lib/fakestore/store"
)

var (
	start           = flag.Bool("start", false, "Start the store service")
	assertFallback  = flag.Bool("assert-fallback", false, "Fallback to the main online store for missing assertions")
	topDir          = flag.String("dir", "", "Directory to be used by the store to keep and serve snaps, <dir>/asserts is used for assertions")
	makeRefreshable = flag.String("make-refreshable", "", "List of snaps with new versions separated by commas")
	addr            = flag.String("addr", "localhost:11028", "Store address")
	https_proxy     = flag.String("https-proxy", "", "HTTPS proxy address")
	http_proxy      = flag.String("http-proxy", "", "HTTP proxy address")
)

func main() {
	if err := logger.SimpleSetup(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to activate logging: %v\n", err)
		os.Exit(1)
	}
	logger.Debugf("fakestore starting")

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	if len(*https_proxy) > 0 {
		os.Setenv("https_proxy", *https_proxy)
	}

	if len(*http_proxy) > 0 {
		os.Setenv("http_proxy", *http_proxy)
	}

	if *start {
		return runServer(*topDir, *addr, *assertFallback)
	}

	if *makeRefreshable != "" {
		return runManage(*topDir, *makeRefreshable)
	}

	return fmt.Errorf("please specify either start or make-refreshable")
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

func runManage(topDir, snaps string) error {
	// setup fake new revisions of snaps for refresh
	snapList := strings.Split(snaps, ",")
	return refresh.MakeFakeRefreshForSnaps(snapList, topDir)
}
