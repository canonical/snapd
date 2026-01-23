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

	"golang.org/x/sync/errgroup"
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
// TODO make this a variant of RunManyWithContext.
func RunWithContext(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

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

	err := cmd.Wait()
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

// RunManyWithContext takes a context, a slice of commands, and a slice of
// functions that accept a context as argument. It uses errgroup to manage the
// lifecycle and error propagation of all tasks. It returns the first non-nil
// error (if any) from the group. The go routines need to periodically look at
// ctx.Done() to be cancellable.
func RunManyWithContext(ctx context.Context, cmds []*exec.Cmd, tasks []func(context.Context) error) error {
	// Create the group and a derived cancellable context
	g, gCtx := errgroup.WithContext(ctx)

	for _, cmd := range cmds {
		c := cmd
		g.Go(func() error {
			if err := c.Start(); err != nil {
				return err
			}
			// Create a channel to wait for the process result
			waitDone := make(chan error, 1)
			go func() {
				waitDone <- c.Wait()
			}()

			// Wait for the context to cancel OR for the process to finish (waitDone)
			select {
			case <-gCtx.Done():
				c.Process.Kill()
				<-waitDone
				return gCtx.Err()
			case err := <-waitDone:
				return err
			}
		})
	}

	for _, task := range tasks {
		t := task
		g.Go(func() error { return t(gCtx) })
	}

	// Return nil or the first error (if any) returned by the spawned go routines
	return g.Wait()
}
