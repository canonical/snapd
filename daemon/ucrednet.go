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

const (
	ucrednetNoProcess = uint32(0)
	ucrednetNobody    = uint32((1 << 32) - 1)
)

func ucrednetGet(remoteAddr string) (pid uint32, uid uint32, socket string, err error) {
	pid = ucrednetNoProcess
	uid = ucrednetNobody
	for _, token := range strings.Split(remoteAddr, ";") {
		var v uint64
		if strings.HasPrefix(token, "pid=") {
			if v, err = strconv.ParseUint(token[4:], 10, 32); err == nil {
				pid = uint32(v)
			} else {
				break
			}
		} else if strings.HasPrefix(token, "uid=") {
			if v, err = strconv.ParseUint(token[4:], 10, 32); err == nil {
				uid = uint32(v)
			} else {
				break
			}
		}
		if strings.HasPrefix(token, "socket=") {
			socket = token[7:]
		}

	}
	if pid == ucrednetNoProcess || uid == ucrednetNobody {
		err = errNoID
	}

	return pid, uid, socket, err
}

type ucrednetAddr struct {
	net.Addr
	pid    string
	uid    string
	socket string
}

func (wa *ucrednetAddr) String() string {
	return fmt.Sprintf("pid=%s;uid=%s;socket=%s;%s", wa.pid, wa.uid, wa.socket, wa.Addr)
}

type ucrednetConn struct {
	net.Conn
	pid    string
	uid    string
	socket string
}

func (wc *ucrednetConn) RemoteAddr() net.Addr {
	return &ucrednetAddr{wc.Conn.RemoteAddr(), wc.pid, wc.uid, wc.socket}
}

type ucrednetListener struct{ net.Listener }

var getUcred = sys.GetsockoptUcred

func (wl *ucrednetListener) Accept() (net.Conn, error) {
	con, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	var pid, uid, socket string
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

		pid = strconv.FormatUint(uint64(ucred.Pid), 10)
		uid = strconv.FormatUint(uint64(ucred.Uid), 10)
		socket = ucon.LocalAddr().String()
	}

	return &ucrednetConn{con, pid, uid, socket}, err
}
