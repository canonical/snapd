// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build statelock

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

package osutil

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

var (
	traceStateLock = false

	traceThreshold = int64(0)
	traceFilePath  = ""
)

func init() {
	err := func() error {
		if !GetenvBool("SNAPPY_TESTING") {
			return fmt.Errorf("SNAPPY_TESTING not set")
		}

		threshold := GetenvInt64("SNAPD_STATE_LOCK_TRACE_THRESHOLD_MS")
		logFilePath := os.Getenv("SNAPD_STATE_LOCK_TRACE_FILE")

		if threshold <= 0 {
			return fmt.Errorf("SNAPD_STATE_LOCK_TRACE_TREHSHOLD_MS is unset, invalid or 0")
		}

		if logFilePath == "" {
			return fmt.Errorf("SNAPD_STATE_LOCK_TRACE_FILE is unset")
		}

		traceThreshold = threshold
		traceFilePath = logFilePath

		return nil
	}()

	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot enable state lock tracing: %v\n", err)
		return
	}

	traceStateLock = true
}

func traceCallers(ts, heldMs, waitMs int64) error {
	if traceFilePath == "" {
		return fmt.Errorf("internal error: trace file path unset")
	}

	logFile, err := os.OpenFile(traceFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("cannot not open/create log trace file: %v", err)
	}
	lockFile := NewFileLockWithFile(logFile)
	defer lockFile.Close()

	if err := lockFile.Lock(); err != nil {
		return fmt.Errorf("cannot take file lock: %v", err)
	}

	pc := make([]uintptr, 10)
	// avoid 3 first callers on the stack: runtime.Callers(), this function and the parent
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])

	_, err = fmt.Fprintf(logFile, "### %s lock: held: %d ms wait %d ms\n",
		time.UnixMilli(ts),
		heldMs, waitMs)
	if err != nil {
		return err
	}

	for {
		frame, more := frames.Next()
		_, err := fmt.Fprintf(logFile, "%s:%d %s\n", frame.File, frame.Line, frame.Function)
		if err != nil {
			return err
		}

		if !more {
			break
		}
	}

	return nil
}

func LockTimestamp() int64 {
	return time.Now().UnixMilli()
}

// MaybeSaveLockTime allows to save lock times when this overpass the threshold
// defined by through the SNAPD_STATE_LOCK_THRESHOLD_MS environment settings.
func MaybeSaveLockTime(lockWait int64, lockStart int64) {
	if !traceStateLock {
		return
	}

	now := LockTimestamp()

	heldMs := now - lockStart
	waitMs := lockStart - lockWait
	if heldMs > traceThreshold || waitMs > traceThreshold {
		if err := traceCallers(now, heldMs, waitMs); err != nil {
			fmt.Fprintf(os.Stderr, "could write state lock trace: %v\n", err)
		}
	}
}
