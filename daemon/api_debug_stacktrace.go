// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"runtime"
)

var (
	runtimeStack = runtime.Stack

	// stacktracesInitialSize is the initial size of the buffer used to
	// collect the stack traces of all goroutines.
	stacktracesInitialSize = 64 * 1024
	// stacktracesMaxSize is the upper bound on the size of the buffer used
	// to collect the stack traces of all goroutines.
	stacktracesMaxSize = 16 * 1024 * 1024
)

const stacktracesTruncatedMarker = "\n... stack traces truncated\n"

func getStacktraces() Response {
	size := stacktracesInitialSize
	for {
		stack := make([]byte, size)
		const all = true
		n := runtimeStack(stack, all)
		if n < len(stack) {
			return SyncResponse(string(stack[:n]))
		}
		if size >= stacktracesMaxSize {
			// the buffer was filled up to the bound, the output is
			// most likely incomplete
			return SyncResponse(string(stack[:n]) + stacktracesTruncatedMarker)
		}
		size *= 2
		if size > stacktracesMaxSize {
			size = stacktracesMaxSize
		}
	}
}
