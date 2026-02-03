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

// RunManyWithContext takes a context and a method with a context argument that
// returns a slice of commands and a slice of functions. It uses errgroup to
// manage the lifecycle and error propagation of all commands/tasks. It returns
// the first non-nil error (if any) from the group. If the commands/tasks are
// expected to be cancellable, buildWithContext should pass the input context
// when creating the commands (using exec.CommandContext) and should ensure
// that tasks listen to ctx.Done() if necessary.
func RunManyWithContext(
	ctx context.Context,
	buildWithContext func(context.Context) (cmds []*exec.Cmd, tasks []func() error, err error),
) error {

	// Create the group and a derived cancellable context
	g, gCtx := errgroup.WithContext(ctx)

	// buildWithContext needs to pass down the errgroup context.
	cmds, tasks, err := buildWithContext(gCtx)
	if err != nil {
		return err
	}

	for _, cmd := range cmds {
		c := cmd
		g.Go(func() error {
			err := c.Run()
			// If cancelled, return that error from the context, as
			// the process error will be just the exit status,
			// providing less information.
			if ctxErr := gCtx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		})
	}

	for _, task := range tasks {
		t := task
		g.Go(func() error { return t() })
	}

	// Return nil or the first error (if any) returned by the spawned go routines
	return g.Wait()
}
