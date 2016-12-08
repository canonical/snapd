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
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	sys "syscall"
)

var errNoUID = errors.New("no uid found")

const ucrednetNobody = uint32((1 << 32) - 1)

func ucrednetGetUID(remoteAddr string) (uint32, error) {
	idx := strings.IndexByte(remoteAddr, ';')
	if !strings.HasPrefix(remoteAddr, "uid=") || idx < 5 {
		return ucrednetNobody, errNoUID
	}

	uid, err := strconv.ParseUint(remoteAddr[4:idx], 10, 32)
	if err != nil {
		return ucrednetNobody, err
	}

	return uint32(uid), nil
}

type ucrednetAddr struct {
	net.Addr
	uid string
}

func (wa *ucrednetAddr) String() string {
	return fmt.Sprintf("uid=%s;%s", wa.uid, wa.Addr)
}

type ucrednetConn struct {
	net.Conn
	uid string
}

func (wc *ucrednetConn) RemoteAddr() net.Addr {
	return &ucrednetAddr{wc.Conn.RemoteAddr(), wc.uid}
}

type ucrednetListener struct{ net.Listener }

var getUcred = sys.GetsockoptUcred

func (wl *ucrednetListener) Accept() (net.Conn, error) {
	con, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	uid := ""
	if ucon, ok := con.(*net.UnixConn); ok {
		f, err := ucon.File()
		if err != nil {
			return nil, err
		}
		// File() is a dup(); needs closing
		defer f.Close()

		ucred, err := getUcred(int(f.Fd()), sys.SOL_SOCKET, sys.SO_PEERCRED)
		if err != nil {
			return nil, err
		}

		uid = strconv.FormatUint(uint64(ucred.Uid), 10)
	}

	return &ucrednetConn{con, uid}, err
}
