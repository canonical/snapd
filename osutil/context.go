// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package osutil

import (
	"context"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
)

// ContextWriter returns a discarding io.Writer which Write method
// returns an error once the context is done.
func ContextWriter(ctx context.Context) io.Writer {
	return ctxWriter{ctx}
}

type ctxWriter struct {
	ctx context.Context
}

func (w ctxWriter) Write(p []byte) (n int, err error) {
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
	}
	return len(p), nil
}

// RunWithContext runs the given command, but kills it if the context
// becomes done before the command finishes.
func RunWithContext(ctx context.Context, cmd *exec.Cmd) error {
	mylog.Check(ctx.Err())
	mylog.Check(cmd.Start())

	var ctxDone uint32
	var wg sync.WaitGroup
	waitDone := make(chan struct{})

	wg.Add(1)
	go func() {
		select {
		case <-ctx.Done():
			atomic.StoreUint32(&ctxDone, 1)
			cmd.Process.Kill()
		case <-waitDone:
		}
		wg.Done()
	}()
	mylog.Check(cmd.Wait())
	close(waitDone)
	wg.Wait()

	if atomic.LoadUint32(&ctxDone) != 0 {
		// do one last check to make sure the error from Wait is what we expect from Kill
		if err, ok := err.(*exec.ExitError); ok {
			if ws, ok := err.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signal() == syscall.SIGKILL {
				return ctx.Err()
			}
		}
	}
	return err
}
