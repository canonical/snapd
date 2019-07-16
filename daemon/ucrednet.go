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
	"regexp"
	"strconv"
	"sync"
	sys "syscall"
)

var errNoID = errors.New("no pid/uid found")

const (
	ucrednetNoProcess = int32(0)
	ucrednetNobody    = uint32((1 << 32) - 1)
)

var raddrRegexp = regexp.MustCompile(`^pid=(\d+);uid=(\d+);socket=([^;]*);$`)

func ucrednetGet(remoteAddr string) (pid int32, uid uint32, socket string, err error) {
	// NOTE treat remoteAddr at one point included a user-controlled
	// string. In case that happens again by accident, treat it as tainted,
	// and be very suspicious of it.
	pid = ucrednetNoProcess
	uid = ucrednetNobody
	subs := raddrRegexp.FindStringSubmatch(remoteAddr)
	if subs != nil {
		if v, err := strconv.ParseInt(subs[1], 10, 32); err == nil {
			pid = int32(v)
		}
		if v, err := strconv.ParseUint(subs[2], 10, 32); err == nil {
			uid = uint32(v)
		}
		socket = subs[3]
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
	// NOTE we drop the original (user-supplied) net.Addr from the
	// serialization entirely. We carry it this far so it helps debugging
	// (via %#v logging), but from here on in it's not helpful.
	return wa.ucrednet.String()
}

type ucrednetConn struct {
	net.Conn
	*ucrednet
}

func (wc *ucrednetConn) RemoteAddr() net.Addr {
	return &ucrednetAddr{wc.Conn.RemoteAddr(), wc.ucrednet}
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

func (wl *ucrednetListener) Close() error {
	wl.idempotClose.Do(func() {
		wl.closeErr = wl.Listener.Close()
	})
	return wl.closeErr
}
