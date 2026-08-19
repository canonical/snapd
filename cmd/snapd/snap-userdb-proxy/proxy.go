// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/varlink/go/varlink"
	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

var maxVarlinkMessageSize = 1 << 10

// snapNameFromPid maps a process id to the instance name of the snap it
// belongs to. It is a variable so it can be mocked in tests.
var snapNameFromPid = cgroup.SnapNameFromPid

// Proxy is the snap-userdb-proxy service. It accepts connections on a
// Unix socket, verifies the peer is a snap by mapping the peer's PID
// (obtained via SO_PEERCRED) to a snap through its cgroup, and
// dispatches varlink method calls to its io.systemd.UserDatabase
// dispatcher (which will forward to io.systemd.Multiplexer once
// Phase 3 lands).
type Proxy struct {
	svc *varlink.Service
}

// NewProxy returns a Proxy with the io.systemd.UserDatabase
// dispatcher registered. Takes an address to which it forwards requests.
func NewProxy(addr string) (*Proxy, error) {
	svc, err := varlink.NewService("Canonical", "snapd", "0", "https://snapcraft.io")
	if err != nil {
		return nil, err
	}

	iface := &userDBIface{targetAddress: addr}
	if err := svc.RegisterInterface(iface); err != nil {
		return nil, err
	}
	return &Proxy{svc: svc}, nil
}

// Serve runs the accept loop until the context is cancelled or the
// listener returns a non-temporary error. Each accepted connection is
// subjected to the peer-identity check (see peerSnapName).
func (p *Proxy) Serve(ctx context.Context, l net.Listener) error {
	// Ensure Accept unblocks when the context is cancelled by
	// closing the listener.
	stopCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	closer, ok := l.(interface{ Close() error })
	if !ok {
		return errors.New("listener does not implement Close")
	}
	go func() {
		<-stopCtx.Done()
		_ = closer.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := l.Accept()
		if err != nil {
			// Wait for outstanding connections to finish
			// before returning.
			wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			p.handleConnection(ctx, c)
		}(conn)
	}
}

// handleConnection performs the peer-identity check and, on success,
// runs the varlink read-dispatch loop for the connection.
func (p *Proxy) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	snapName, err := peerSnapName(conn)
	if err != nil || snapName == "" {
		logger.Debugf("cannot determine socket peer to be a snap: %v", err)
		return
	}
	logger.Noticef("accepted connection from snap peer %q", snapName)

	cconn := newCtxConn(conn)
	nRequests := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		body, err := cconn.ReadMessage(ctx)
		if err != nil {
			logger.Debugf("read ended after %d request(s): %v", nRequests, err)
			return
		}
		nRequests++
		if err := p.svc.HandleMessage(ctx, cconn, body); err != nil {
			logger.Noticef("dispatch failed: %v", err)
			return
		}
	}
}

// activationListener returns a listener for the socket at
func activationListener() (net.Listener, error) {
	pidStr, ok := os.LookupEnv("LISTEN_PID")
	if !ok {
		return nil, errors.New("snap-userdb-proxy must be socket-activated (LISTEN_PID is unset)")
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_PID %q: %v", pidStr, err)
	}
	if pid != os.Getpid() {
		return nil, fmt.Errorf("LISTEN_PID %d does not match current pid %d", pid, os.Getpid())
	}
	nfdsStr, ok := os.LookupEnv("LISTEN_FDS")
	if !ok {
		return nil, errors.New("snap-userdb-proxy must be socket-activated (LISTEN_FDS is unset)")
	}
	nfds, err := strconv.Atoi(nfdsStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_FDS %q: %v", nfdsStr, err)
	}
	if nfds != 1 {
		return nil, fmt.Errorf("snap-userdb-proxy expects exactly one listen fd, got %d", nfds)
	}

	const systemdFirstSocketActivationFd = 3
	f := os.NewFile(uintptr(systemdFirstSocketActivationFd), "listen")
	l, err := net.FileListener(f)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return l, nil
}

// ctxConn adapts a net.Conn to varlink's ReadWriterContext interface.
// It intentionally does not implement full context-driven cancelation
// (a simple deadline-based cancelation is enough for a proxy that
// handles short-lived, request/response conversations) but honours the
// context by setting deadlines when the context is cancelled.
type ctxConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func newCtxConn(c net.Conn) *ctxConn {
	return &ctxConn{
		conn: c,
		r:    bufio.NewReader(c),
	}
}

// aLongTimeAgo is used to unblock in-progress Read/Write when the
// context is cancelled.
var aLongTimeAgo = time.Unix(1, 0)

// applyDeadline applies the context deadline to the underlying connection and
// forces the deadline into the past if the context is cancelled.
func (c *ctxConn) applyDeadline(ctx context.Context, set func(time.Time) error) func() {
	dl, ok := ctx.Deadline()
	if ok {
		_ = set(dl)
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = set(aLongTimeAgo)
		case <-stop:
		}
	}()
	return func() {
		close(stop)
		if !ok {
			_ = set(time.Time{})
		}
	}
}

func (c *ctxConn) Read(ctx context.Context, buf []byte) (int, error) {
	done := c.applyDeadline(ctx, c.conn.SetReadDeadline)
	defer done()
	return c.conn.Read(buf)
}

func (c *ctxConn) Write(ctx context.Context, buf []byte) (int, error) {
	done := c.applyDeadline(ctx, c.conn.SetWriteDeadline)
	defer done()
	return c.conn.Write(buf)
}

func (c *ctxConn) ReadBytes(ctx context.Context, delim byte) ([]byte, error) {
	done := c.applyDeadline(ctx, c.conn.SetReadDeadline)
	defer done()
	return c.r.ReadBytes(delim)
}

func (c *ctxConn) ReadMessage(ctx context.Context) ([]byte, error) {
	done := c.applyDeadline(ctx, c.conn.SetReadDeadline)
	defer done()

	var buf bytes.Buffer
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == 0 {
			return buf.Bytes(), nil
		}
		if buf.Len() > maxVarlinkMessageSize {
			return nil, fmt.Errorf("varlink message too large")
		}
		buf.WriteByte(b)
	}
}

// getPeerUcred returns the peer credentials of the given connection.
var getPeerUcred = func(conn *net.UnixConn) (*unix.Ucred, error) {
	f, err := conn.File()
	if err != nil {
		return nil, fmt.Errorf("cannot get file descriptor for connection: %v", err)
	}
	defer f.Close()

	return unix.GetsockoptUcred(int(f.Fd()), syscall.SOL_SOCKET, unix.SO_PEERCRED)
}

// peerSnapName returns the instance name if the socket peer is a snap. If not,
// returns an error.
func peerSnapName(conn net.Conn) (string, error) {
	uconn, ok := conn.(*net.UnixConn)
	if !ok {
		return "", fmt.Errorf("expected AF_UNIX peer, got %T", conn)
	}

	ucred, err := getPeerUcred(uconn)
	if err != nil {
		return "", err
	}

	return snapNameFromPid(int(ucred.Pid))
}
