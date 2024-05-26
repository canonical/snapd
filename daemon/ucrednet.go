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
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	sys "syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

var errNoID = errors.New("no pid/uid found")

const (
	ucrednetNoProcess = int32(0)
	ucrednetNobody    = uint32((1 << 32) - 1)
)

var raddrRegexp = regexp.MustCompile(`^pid=(\d+);uid=(\d+);socket=([^;]*);(iface=([^;]*);)?$`)

var (
	ucrednetGet               = ucrednetGetImpl
	ucrednetGetWithInterfaces = ucrednetGetWithInterfacesImpl
)

func ucrednetGetImpl(remoteAddr string) (*ucrednet, error) {
	uc, _ := mylog.Check3(ucrednetGetWithInterfaces(remoteAddr))
	return uc, err
}

func ucrednetGetWithInterfacesImpl(remoteAddr string) (ucred *ucrednet, ifaces []string, err error) {
	// NOTE treat remoteAddr at one point included a user-controlled
	// string. In case that happens again by accident, treat it as tainted,
	// and be very suspicious of it.
	u := &ucrednet{
		Pid: ucrednetNoProcess,
		Uid: ucrednetNobody,
	}
	subs := raddrRegexp.FindStringSubmatch(remoteAddr)
	if subs != nil {
		if v := mylog.Check2(strconv.ParseInt(subs[1], 10, 32)); err == nil {
			u.Pid = int32(v)
		}
		if v := mylog.Check2(strconv.ParseUint(subs[2], 10, 32)); err == nil {
			u.Uid = uint32(v)
		}
		// group: ([^;]*) - socket path following socket=
		u.Socket = subs[3]
		// group: (iface=([^;]*);)
		if len(subs[4]) > 0 {
			// group: ([^;]*) - actual interfaces joined together with & separator
			ifaces = strings.Split(subs[5], "&")
		}
	}
	if u.Pid == ucrednetNoProcess || u.Uid == ucrednetNobody {
		return nil, nil, errNoID
	}

	return u, ifaces, nil
}

func ucrednetAttachInterface(remoteAddr, iface string) string {
	inds := raddrRegexp.FindStringSubmatchIndex(remoteAddr)
	if inds == nil {
		// This should only occur if remoteAddr is invalid.
		return fmt.Sprintf("%siface=%s;", remoteAddr, iface)
	}
	// start of string matching group "(iface=([^;]*);)"
	ifaceSubStart := inds[8]
	ifaceSubEnd := inds[9]
	if ifaceSubStart == ifaceSubEnd {
		// "(iface=([^;]*);)" not present.
		return fmt.Sprintf("%siface=%s;", remoteAddr, iface)
	}
	// string matching group "([^;]*)" within "(iface=([^;]*);)"
	ifacesStr := remoteAddr[inds[10]:inds[11]]
	ifaces := strings.Split(ifacesStr, "&")
	if strutil.ListContains(ifaces, iface) {
		return remoteAddr
	}
	ifaces = append(ifaces, iface)
	return fmt.Sprintf("%siface=%s;", remoteAddr[:ifaceSubStart], strings.Join(ifaces, "&"))
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
	con := mylog.Check2(wl.Listener.Accept())

	var unet *ucrednet
	if ucon, ok := con.(*net.UnixConn); ok {
		syscallConn := mylog.Check2(ucon.SyscallConn())

		var ucred *sys.Ucred
		scErr := syscallConn.Control(func(fd uintptr) {
			ucred = mylog.Check2(getUcred(int(fd), sys.SOL_SOCKET, sys.SO_PEERCRED))
		})
		if scErr != nil {
			return nil, scErr
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
