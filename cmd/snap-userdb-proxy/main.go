// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

// Implements snap-userdb-proxy, a small varlink service that proxies io.systemd.Multiplexer
// for confined snaps to query for user records.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snapcore/snapd/logger"
)

func main() {
	logger.SimpleSetup(nil)

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "snap-userdb-proxy: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	const defaultAddr = "unix:/run/systemd/userdb/io.systemd.Multiplexer"
	proxy, err := NewProxy(defaultAddr)
	if err != nil {
		return fmt.Errorf("cannot construct proxy: %v", err)
	}

	l, err := activationListener()
	if err != nil {
		return err
	}
	defer l.Close()

	logger.Debugf("snap-userdb-proxy listening on %q", l.Addr())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Noticef("received signal %v, shutting down", sig)
		cancel()
	}()

	return proxy.Serve(ctx, l)
}
