// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"fmt"
	"net"
	sys "syscall"
)

type wrapnixAddr struct {
	net.UnixAddr
	ucred *sys.Ucred
}

func (wa *wrapnixAddr) String() string {
	return fmt.Sprintf("%d:%s", wa.ucred.Uid, wa.UnixAddr.String())
}

type wrapnixConn struct {
	net.UnixConn
	ucred *sys.Ucred
}

func (wc *wrapnixConn) RemoteAddr() net.Addr {
	return &wrapnixAddr{*wc.UnixConn.RemoteAddr().(*net.UnixAddr), wc.ucred}
}

type wrapnixListener struct{ net.UnixListener }

var getUcred = sys.GetsockoptUcred

func (wl *wrapnixListener) Accept() (net.Conn, error) {
	con, err := wl.UnixListener.Accept()
	if err != nil {
		return nil, err
	}

	ucon := con.(*net.UnixConn)
	f, err := ucon.File()
	if err != nil {
		return nil, err
	}

	ucred, err := getUcred(int(f.Fd()), sys.SOL_SOCKET, sys.SO_PEERCRED)
	if err != nil {
		return nil, err
	}

	return &wrapnixConn{*ucon, ucred}, err
}
