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

var errNoID = errors.New("no pid/uid found")

const ucrednetNoProcess = uint64(0)
const ucrednetNobody = uint32((1 << 32) - 1)

func ucrednetGet(remoteAddr string) (uint64, uint32, error) {
	idx := strings.IndexByte(remoteAddr, ';')
	if !strings.HasPrefix(remoteAddr, "pid=") || idx < 5 {
		return ucrednetNoProcess, ucrednetNobody, errNoID
	}

	pid, err := strconv.ParseUint(remoteAddr[4:idx], 10, 64)
	if err != nil {
		return ucrednetNoProcess, ucrednetNobody, err
	}

	s := remoteAddr[idx+1:]
	idx2 := strings.IndexByte(s, ';')
	if !strings.HasPrefix(s, "uid=") || idx2 < 5 {
		return ucrednetNoProcess, ucrednetNobody, errNoID
	}

	uid, err := strconv.ParseUint(s[4:idx2], 10, 32)
	if err != nil {
		return ucrednetNoProcess, ucrednetNobody, err
	}

	return uint64(pid), uint32(uid), nil
}

type ucrednetAddr struct {
	net.Addr
	pid string
	uid string
}

func (wa *ucrednetAddr) String() string {
	return fmt.Sprintf("pid=%s;uid=%s;%s", wa.pid, wa.uid, wa.Addr)
}

type ucrednetConn struct {
	net.Conn
	pid string
	uid string
}

func (wc *ucrednetConn) RemoteAddr() net.Addr {
	return &ucrednetAddr{wc.Conn.RemoteAddr(), wc.pid, wc.uid}
}

type ucrednetListener struct{ net.Listener }

var getUcred = sys.GetsockoptUcred

func (wl *ucrednetListener) Accept() (net.Conn, error) {
	con, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	pid := ""
	uid := ""
	if ucon, ok := con.(*net.UnixConn); ok {
		f, err := ucon.File()
		if err != nil {
			return nil, err
		}

		ucred, err := getUcred(int(f.Fd()), sys.SOL_SOCKET, sys.SO_PEERCRED)
		if err != nil {
			return nil, err
		}

		pid = strconv.FormatUint(uint64(ucred.Pid), 10)
		uid = strconv.FormatUint(uint64(ucred.Uid), 10)
	}

	return &ucrednetConn{con, pid, uid}, err
}
