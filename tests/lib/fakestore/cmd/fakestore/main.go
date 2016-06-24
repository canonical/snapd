// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

	"github.com/snapcore/snapd/tests/lib/fakestore/refresh"
	"github.com/snapcore/snapd/tests/lib/fakestore/store"
)

var (
	action  = flag.String("action", "start", "Action to be performed, one of start and manage")
	blobDir = flag.String("blobdir", "", "Directory to be used by the store to keep snaps")
	snaps   = flag.String("snaps", "", "List of snaps with new versions separated by commas")
	addr    = flag.String("addr", "locahost:11028", "Store address")

	st *store.Store
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	if *action == "start" {
		return runServer(*blobDir, *addr)
	}

	if *action == "manage" {
		return runManage(*blobDir, *snaps)
	}

	return fmt.Errorf("unknown action: %s", *action)
}

func runServer(blobDir, addr string) error {
	st = store.NewStore(blobDir, addr)

	if err := st.Start(); err != nil {
		return err
	}

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch

	return st.Stop()
}

func runManage(blobDir, snaps string) error {
	// setup snaps
	snapList := strings.Split(snaps, ",")
	return refresh.CallFakeSnap(snapList, blobDir)
}
