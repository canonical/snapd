// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

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
 */

package userbus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
)

// RunBridge is the internal snapd-userbus entry point. It takes ownership of
// peer and must run in a separately exec'd process with the target credentials.
// It returns when either side disconnects or ctx is canceled.
func RunBridge(ctx context.Context, uid uint32, peer *os.File) error {
	client, err := fileConn(peer)
	if err != nil {
		return err
	}
	defer client.Close()
	if uint32(os.Getuid()) != uid || uint32(os.Geteuid()) != uid {
		return bridgeError(client, fmt.Errorf("user bus bridge credentials do not match uid %d", uid))
	}
	path := filepath.Join(dirs.XdgRuntimeDirBase, strconv.FormatUint(uint64(uid), 10), "bus")
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return bridgeError(client, fmt.Errorf("cannot connect to user bus: %w", err))
	}
	bus := conn.(*net.UnixConn)
	defer bus.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
			bus.Close()
		case <-done:
		}
	}()
	var ready [len(readyMagic) + 4]byte
	copy(ready[:], readyMagic)
	binary.LittleEndian.PutUint32(ready[len(readyMagic):], uid)
	if _, err := client.Write(ready[:]); err != nil {
		return err
	}
	// The parent's initial NUL carries root's SCM_CREDENTIALS. Consume it
	// and originate a NUL with this process's credentials on the user bus.
	var nul [1]byte
	if _, err := io.ReadFull(client, nul[:]); err != nil {
		return err
	}
	if nul[0] != 0 {
		return fmt.Errorf("invalid initial D-Bus byte")
	}
	credentials := unix.UnixCredentials(&unix.Ucred{Pid: int32(os.Getpid()), Uid: uid, Gid: uint32(os.Getgid())})
	if _, _, err := bus.WriteMsgUnix(nul[:], credentials, nil); err != nil {
		return err
	}
	if err := relayAuthentication(bus, client); err != nil {
		return err
	}
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- relay(bus, client) }()
	go func() { errorsCh <- relay(client, bus) }()
	err = <-errorsCh
	client.Close()
	bus.Close()
	<-errorsCh
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// Report setup errors on the socket before closing it. Stderr alone races with
// the parent observing EOF and terminating the helper.
func bridgeError(client *net.UnixConn, err error) error {
	message := []byte(err.Error())
	if len(message) > 4096 {
		message = message[:4096]
	}
	var header [len(errorMagic) + 4]byte
	copy(header[:], errorMagic)
	binary.LittleEndian.PutUint32(header[len(errorMagic):], uint32(len(message)))
	client.Write(append(header[:], message...))
	return err
}
