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

package userbus_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/usersession/userbus"
)

func (s *connectionSuite) TestConnectionOutlivesCallingThread(c *C) {
	uid := uint32(os.Getuid())
	s.startBus(c, uid)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		conn *userbus.Connection
		err  error
		tid  int
	}
	connected := make(chan result, 1)
	finished := make(chan struct{})
	defer close(finished)
	var connectOnThread func()
	connectOnThread = func() {
		// Deliberately leave the thread locked: Go destroys it when this
		// goroutine exits. The connection must belong to the process, not
		// to this short-lived thread.
		runtime.LockOSThread()
		tid := unix.Gettid()
		if tid == os.Getpid() {
			// Go retains its initial thread even when a locked goroutine
			// exits. Reserve it while exercising a disposable worker thread.
			go connectOnThread()
			<-finished
			runtime.UnlockOSThread()
			return
		}
		conn, err := userbus.Connect(ctx, uid)
		connected <- result{conn, err, tid}
	}
	go connectOnThread()
	r := <-connected
	c.Assert(r.err, IsNil)
	defer r.conn.Close()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		err := unix.Tgkill(os.Getpid(), r.tid, 0)
		if errors.Is(err, unix.ESRCH) {
			break
		}
		c.Assert(err, IsNil)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			c.Fatal("calling thread did not exit")
		}
	}
	s.checkIdentity(c, r.conn, uid)
}

func runParentDeathHelper() {
	root := os.Args[2]
	dirs.SetRootDir(root)
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	userbus.MockInternalLibExecDir(func() (string, error) { return "/unused", nil })
	userbus.MockCommandContext(func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, exe, append(args, root)...)
	})
	conn, err := userbus.Connect(context.Background(), uint32(os.Getuid()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conn.Close()
}

func (s *connectionSuite) TestParentDeathDuringAuthentication(c *C) {
	path := filepath.Join(dirs.XdgRuntimeDirBase, strconv.Itoa(os.Getuid()), "bus")
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	c.Assert(err, IsNil)
	defer listener.Close()
	c.Assert(listener.SetDeadline(time.Now().Add(5*time.Second)), IsNil)
	exe, err := os.Executable()
	c.Assert(err, IsNil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parent := exec.CommandContext(ctx, exe, "userbus-parent-death", s.root)
	parent.Stderr = os.Stderr
	c.Assert(parent.Start(), IsNil)
	defer func() {
		parent.Process.Kill()
		parent.Wait()
	}()
	peer, err := listener.AcceptUnix()
	c.Assert(err, IsNil)
	defer peer.Close()
	c.Assert(peer.SetReadDeadline(time.Now().Add(5*time.Second)), IsNil)
	var auth [7]byte
	_, err = io.ReadFull(peer, auth[:])
	c.Assert(err, IsNil)
	c.Assert(string(auth[:]), Equals, "\x00AUTH\r\n")
	// The helper is waiting for a server response, not watching client EOF.
	// Terminating the parent must nevertheless close its upstream socket.
	c.Assert(parent.Process.Kill(), IsNil)
	var b [1]byte
	_, err = peer.Read(b[:])
	c.Check(errors.Is(err, io.EOF), Equals, true)
}
