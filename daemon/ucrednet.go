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
	ucrednetNoProcess = int32(0)
	ucrednetNobody    = uint32((1 << 32) - 1)
)

func ucrednetGet(remoteAddr string) (pid int32, uid uint32, socket string, err error) {
	pid = ucrednetNoProcess
	uid = ucrednetNobody
	for _, token := range strings.Split(remoteAddr, ";") {
		if strings.HasPrefix(token, "pid=") {
			var v int64
			if v, err = strconv.ParseInt(token[4:], 10, 32); err == nil {
				pid = int32(v)
			} else {
				break
			}
		} else if strings.HasPrefix(token, "uid=") {
			var v uint64
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

type ucrednet struct {
	pid    int32
	uid    uint32
	socket string
}

func (un *ucrednet) String() string {
	if un == nil {
		return "pid=;uid=;socket=;"
	}
	return fmt.Sprintf("pid=%d;uid=%d;socket=%s;", un.pid, un.uid, un.socket)
}

type ucrednetAddr struct {
	net.Addr
	*ucrednet
}

func (wa *ucrednetAddr) String() string {
	return wa.ucrednet.String()
}

type ucrednetConn struct {
	net.Conn
	*ucrednet
}

func (wc *ucrednetConn) RemoteAddr() net.Addr {
	return &ucrednetAddr{wc.Conn.RemoteAddr(), wc.ucrednet}
}

type ucrednetListener struct{ net.Listener }

var getUcred = sys.GetsockoptUcred

func (wl *ucrednetListener) Accept() (net.Conn, error) {
	con, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	var unet *ucrednet
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

		unet = &ucrednet{
			pid:    ucred.Pid,
			uid:    ucred.Uid,
			socket: ucon.LocalAddr().String(),
		}
	}

	return &ucrednetConn{con, unet}, nil
}
