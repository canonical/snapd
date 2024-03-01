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

var raddrRegexp = regexp.MustCompile(`^pid=(\d+);uid=(\d+);socket=([^;]*);(iface=([^;]*);)?$`)

var ucrednetGet = ucrednetGetImpl
var ucrednetGetWithInterface = ucrednetGetWithInterfaceImpl

func ucrednetGetImpl(remoteAddr string) (*ucrednet, error) {
	uc, _, err := ucrednetGetWithInterface(remoteAddr)
	return uc, err
}

func ucrednetGetWithInterfaceImpl(remoteAddr string) (ucred *ucrednet, iface string, err error) {
	// NOTE treat remoteAddr at one point included a user-controlled
	// string. In case that happens again by accident, treat it as tainted,
	// and be very suspicious of it.
	u := &ucrednet{
		Pid: ucrednetNoProcess,
		Uid: ucrednetNobody,
	}
	subs := raddrRegexp.FindStringSubmatch(remoteAddr)
	if subs != nil {
		if v, err := strconv.ParseInt(subs[1], 10, 32); err == nil {
			u.Pid = int32(v)
		}
		if v, err := strconv.ParseUint(subs[2], 10, 32); err == nil {
			u.Uid = uint32(v)
		}
		u.Socket = subs[3]
		if len(subs) == 6 {
			iface = subs[5]
		}
	}
	if u.Pid == ucrednetNoProcess || u.Uid == ucrednetNobody {
		return nil, "", errNoID
	}

	return u, iface, nil
}

func ucrednetAttachInterface(remoteAddr, iface string) string {
	return fmt.Sprintf("%siface=%s;", remoteAddr, iface)
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
