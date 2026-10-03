// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

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
 */

// Package userbus connects to another user's regular D-Bus session bus through
// a credential-switched snapd helper. It does not establish a login session.
package userbus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/snapdtool"
)

const (
	helperName   = "snapd-userbus"
	readyMagic   = "SNAPBUS1"
	errorMagic   = "SNAPERR1"
	setupTimeout = 30 * time.Second
)

var (
	commandContext     = exec.CommandContext
	lookupUser         = user.LookupId
	geteuid            = os.Geteuid
	internalLibExecDir = snapdtool.InternalLibExecDir
)

// Connection owns a private godbus connection and its bridge process. Use the
// embedded Conn for method calls and signals, and Connection.Close for cleanup.
type Connection struct {
	*dbus.Conn
	bridge *bridgeProcess
}

// Close closes the connection and reaps its helper. It is safe to call more
// than once or concurrently. Closing does not stop services already submitted
// to the user manager.
func (c *Connection) Close() error {
	c.bridge.stop()
	err := c.Conn.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

type bridgeProcess struct {
	wire       *net.UnixConn
	cancel     context.CancelFunc
	done       chan struct{}
	err        error             // written before closing done
	diagnostic limitedDiagnostic // read only after done
}

func (p *bridgeProcess) stop() {
	// Close the socket before godbus.Close: a blocked send must be unblocked
	// before godbus can acquire its output-handler lock during shutdown.
	p.wire.Close()
	p.cancel()
	<-p.done
}

// Connect connects to /run/user/<uid>/bus. Root may select any existing user;
// other callers may only select themselves. The bus must already exist.
//
// The context governs the entire connection lifetime. Callers should use
// separate contexts for individual D-Bus calls. Setup I/O is additionally
// bounded to 30 seconds. No system bus fallback or session autolaunch is used.
// Unix FD negotiation must succeed. The caller must eventually call Close.
// Options can install a signal handler or other godbus connection behavior;
// authentication is always EXTERNAL through the credential-switched helper.
func Connect(ctx context.Context, uid uint32, options ...dbus.ConnOption) (*Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmdCtx, cancel := context.WithCancel(ctx)
	cmd, err := helperCommand(cmdCtx, uid)
	if err != nil {
		cancel()
		return nil, err
	}
	wire, child, err := socketPair()
	if err != nil {
		cancel()
		return nil, err
	}
	defer child.Close()
	p := &bridgeProcess{wire: wire, cancel: cancel, done: make(chan struct{})}
	cmd.ExtraFiles = []*os.File{child}
	cmd.Stderr = &p.diagnostic
	cmd.WaitDelay = time.Second
	started := make(chan error)
	go func() {
		// Linux ties Pdeathsig to the creating OS thread. Keep that thread
		// alive until the helper exits, independently of the caller's
		// goroutine and any thread it may have locked.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := cmd.Start()
		started <- err
		if err == nil {
			p.err = cmd.Wait()
		}
		wire.Close()
		cancel()
		close(p.done)
	}()
	if err := <-started; err != nil {
		p.stop()
		return nil, fmt.Errorf("cannot start user bus bridge: %w", err)
	}
	child.Close()
	go func() {
		<-cmdCtx.Done()
		wire.Close()
	}()

	conn, err := connectBridge(wire, uid, options...)
	if err != nil {
		p.stop()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		detail := strings.TrimSpace(p.diagnostic.String())
		if detail == "" && p.err != nil {
			detail = p.err.Error()
		}
		if detail != "" {
			return nil, fmt.Errorf("cannot connect to user %d bus: %w (%s)", uid, err, detail)
		}
		return nil, fmt.Errorf("cannot connect to user %d bus: %w", uid, err)
	}
	return &Connection{Conn: conn, bridge: p}, nil
}

func connectBridge(wire *net.UnixConn, uid uint32, options ...dbus.ConnOption) (*dbus.Conn, error) {
	if err := wire.SetDeadline(time.Now().Add(setupTimeout)); err != nil {
		return nil, err
	}
	// The helper writes this fixed-size preamble before relaying any D-Bus
	// traffic. Do not buffer reads across the boundary into authentication.
	var ready [len(readyMagic) + 4]byte
	if _, err := io.ReadFull(wire, ready[:]); err != nil {
		return nil, err
	}
	if string(ready[:len(readyMagic)]) == errorMagic {
		n := binary.LittleEndian.Uint32(ready[len(readyMagic):])
		if n > 4096 {
			return nil, fmt.Errorf("oversized user bus bridge error")
		}
		message := make([]byte, n)
		if _, err := io.ReadFull(wire, message); err != nil {
			return nil, err
		}
		return nil, errors.New(string(message))
	}
	if string(ready[:len(readyMagic)]) != readyMagic || binary.LittleEndian.Uint32(ready[len(readyMagic):]) != uid {
		return nil, fmt.Errorf("invalid user bus bridge greeting")
	}
	options = append(options, dbus.WithAuth(dbus.AuthExternal("")))
	conn, err := dbus.ConnectUnix(wire, options...)
	if err != nil {
		return nil, err
	}
	if !conn.SupportsUnixFDs() {
		conn.Close()
		return nil, fmt.Errorf("user bus does not support file descriptor passing")
	}
	if err := wire.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func helperCommand(ctx context.Context, uid uint32) (*exec.Cmd, error) {
	if uid == ^uint32(0) {
		return nil, fmt.Errorf("invalid user id")
	}
	if geteuid() != 0 && uint32(geteuid()) != uid {
		return nil, fmt.Errorf("cannot connect to another user's bus without root privileges")
	}
	account, err := lookupUser(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return nil, fmt.Errorf("cannot find user %d: %w", uid, err)
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot parse primary group for user %d: %w", uid, err)
	}
	if gid == uint64(^uint32(0)) {
		return nil, fmt.Errorf("invalid primary group for user %d", uid)
	}
	// Resolve the directory, not InternalToolPath's fallback to the distro
	// executable: the helper must come from the invoking snapd's tree. Use
	// the snapd entry point so FIPS builds still go through their dispatcher.
	libexec, err := internalLibExecDir()
	if err != nil {
		return nil, err
	}
	cmd := commandContext(ctx, filepath.Join(libexec, "snapd"), helperName, strconv.FormatUint(uint64(uid), 10))
	cmd.Args[0] = "snapd"
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "SNAP_REEXEC=0"}
	// Also terminate during setup if the parent dies while the upstream bus
	// is unresponsive and there is not yet a reader watching the client EOF.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if geteuid() == 0 {
		// An empty Groups slice clears root's supplementary groups. Services
		// started through this bus get their groups from the user manager.
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: uid, Gid: uint32(gid), Groups: []uint32{},
		}
	}
	return cmd, nil
}

type limitedDiagnostic struct{ strings.Builder }

func (b *limitedDiagnostic) Write(p []byte) (int, error) {
	n := len(p)
	if left := 4096 - b.Len(); left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		b.Builder.Write(p)
	}
	return n, nil
}
