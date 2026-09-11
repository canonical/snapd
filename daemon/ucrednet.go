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
	"os"
	"sync"
	sys "syscall"

	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/strutil"
)

var errNoID = errors.New("no peer credentials found")

type ucrednetContextKey struct{}
type ucrednetInterfacesContextKey struct{}

func ucrednetWithCredentials(ctx context.Context, ucred *ucrednet) context.Context {
	if ucred == nil {
		return ctx
	}
	return context.WithValue(ctx, ucrednetContextKey{}, *ucred)
}

// ucrednetConnContext is provided as snapd's [http.Server.ConnContext]. If the
// connection if of type [ucrednetConn], then we attach a [ucrednet] to the
// connection's [context.Context]. Each HTTP request context is dervived from
// this context.
func ucrednetConnContext(ctx context.Context, conn net.Conn) context.Context {
	uconn, ok := conn.(*ucrednetConn)
	if !ok {
		return ctx
	}
	return ucrednetWithCredentials(ctx, uconn.ucrednet)
}

// ucrednetGet attempts to read the [ucrednet] associated with an HTTP request's
// context. This will be attached to each HTTP request that is served by a
// [ucrednetListener] [net.Listener].
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
	instanceName    string
	instanceNameErr error
	// Uid is the peer user ID obtained from the socket credentials.
	Uid uint32
	// Socket is the local Unix socket path on which the connection was
	// accepted.
	Socket string

	untrustedProcessExeName    string
	untrustedProcessExeNameErr error
	// PolkitPID is the peer PID, should only be used for polkit authorization.
	PolkitPID int32
}

// InstanceName returns the peer snap instance name captured at acceptance.
// It returns an error if the name is not available.
func (un *ucrednet) InstanceName() (string, error) {
	if un.instanceName == "" && un.instanceNameErr == nil {
		return "", errors.New("snap instance name is not available")
	}
	return un.instanceName, un.instanceNameErr
}

// UntrustedProcessExeName returns the peer executable path captured at
// acceptance. This path must not be used for security checks.
// It returns an error if the path is not available.
func (un *ucrednet) UntrustedProcessExeName() (string, error) {
	if un.untrustedProcessExeName == "" && un.untrustedProcessExeNameErr == nil {
		return "", errors.New("process executable name is not available")
	}
	return un.untrustedProcessExeName, un.untrustedProcessExeNameErr
}

func (un *ucrednet) String() string {
	if un == nil {
		return "snap=;uid=;socket=;"
	}
	return fmt.Sprintf("snap=%s;uid=%d;socket=%s;", un.instanceName, un.Uid, un.Socket)
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
var osReadlink = os.Readlink
var cgroupSnapNameFromPid = cgroup.SnapNameFromPid

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
			Uid:       ucred.Uid,
			Socket:    ucon.LocalAddr().String(),
			PolkitPID: ucred.Pid,
		}
		// non-snap clients and failed lookups must not prevent serving the connection.
		unet.instanceName, unet.instanceNameErr = cgroupSnapNameFromPid(int(ucred.Pid))
		// an unreadable executable must not prevent the connection from being served.
		unet.untrustedProcessExeName, unet.untrustedProcessExeNameErr = osReadlink(fmt.Sprintf("/proc/%d/exe", ucred.Pid))
	}

	return &ucrednetConn{con, unet}, nil
}

func (wl *ucrednetListener) Close() error {
	wl.idempotClose.Do(func() {
		wl.closeErr = wl.Listener.Close()
	})
	return wl.closeErr
}
