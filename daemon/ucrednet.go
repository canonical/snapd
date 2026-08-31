// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2024 Canonical Ltd
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
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	sys "syscall"

	"github.com/snapcore/snapd/strutil"
)

var errNoID = errors.New("no pid/uid found")

type ucrednetContextKey struct{}
type ucrednetInterfacesContextKey struct{}

func ucrednetWithCredentials(ctx context.Context, ucred *ucrednet) context.Context {
	if ucred == nil {
		return ctx
	}
	return context.WithValue(ctx, ucrednetContextKey{}, *ucred)
}

func ucrednetConnContext(ctx context.Context, conn net.Conn) context.Context {
	uconn, ok := conn.(*ucrednetConn)
	if !ok {
		return ctx
	}
	return ucrednetWithCredentials(ctx, uconn.ucrednet)
}

func ucrednetGet(ctx context.Context) (*ucrednet, error) {
	ucred, ok := ctx.Value(ucrednetContextKey{}).(ucrednet)
	if !ok {
		return nil, errNoID
	}
	return &ucred, nil
}

func ucrednetGetWithInterfaces(ctx context.Context) (ucred *ucrednet, ifaces []string, err error) {
	ucred, err = ucrednetGet(ctx)
	if err != nil {
		return nil, nil, err
	}
	ifaces, _ = ctx.Value(ucrednetInterfacesContextKey{}).([]string)
	return ucred, append([]string(nil), ifaces...), nil
}

func ucrednetAttachInterface(ctx context.Context, iface string) context.Context {
	ifaces, _ := ctx.Value(ucrednetInterfacesContextKey{}).([]string)
	if strutil.ListContains(ifaces, iface) {
		return ctx
	}
	updated := make([]string, len(ifaces), len(ifaces)+1)
	copy(updated, ifaces)
	updated = append(updated, iface)
	return context.WithValue(ctx, ucrednetInterfacesContextKey{}, updated)
}

type ucrednet struct {
	Pid    int32
	Uid    uint32
	Socket string
}

func (un *ucrednet) String() string {
	if un == nil {
		return "pid=;uid=;socket=;"
	}
	return fmt.Sprintf("pid=%d;uid=%d;socket=%s;", un.Pid, un.Uid, un.Socket)
}

type ucrednetConn struct {
	net.Conn
	*ucrednet
}

type ucrednetListener struct {
	net.Listener

	idempotClose sync.Once
	closeErr     error
}

var getUcred = sys.GetsockoptUcred

func (wl *ucrednetListener) Accept() (net.Conn, error) {
	con, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	var unet *ucrednet
	if ucon, ok := con.(*net.UnixConn); ok {
		syscallConn, err := ucon.SyscallConn()
		if err != nil {
			return nil, err
		}

		var ucred *sys.Ucred
		scErr := syscallConn.Control(func(fd uintptr) {
			ucred, err = getUcred(int(fd), sys.SOL_SOCKET, sys.SO_PEERCRED)
		})
		if scErr != nil {
			return nil, scErr
		}
		if err != nil {
			return nil, err
		}

		unet = &ucrednet{
			Pid:    ucred.Pid,
			Uid:    ucred.Uid,
			Socket: ucon.LocalAddr().String(),
		}
	}

	return &ucrednetConn{con, unet}, nil
}

func (wl *ucrednetListener) Close() error {
	wl.idempotClose.Do(func() {
		wl.closeErr = wl.Listener.Close()
	})
	return wl.closeErr
}
