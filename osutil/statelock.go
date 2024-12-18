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

func traceCallers(description string) {
	lockfilePath := os.Getenv("SNAPD_STATE_LOCK_FILE")
	if lockfilePath == "" {
		fmt.Fprintf(os.Stderr, "could not retrieve log file, SNAPD_STATE_LOCK_FILE env var required")
	}

	lockfile, err := os.OpenFile(lockfilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not open/create log traces file: %v", err)
		return
	}
	defer lockfile.Close()

	pc := make([]uintptr, 10)
	n := runtime.Callers(0, pc)
	formattedLine := fmt.Sprintf("##%s\n", description)
	if _, err = lockfile.WriteString(formattedLine); err != nil {
		fmt.Fprintf(os.Stderr, "internal error: could not write trace callers header to tmp file: %v", err)
		return
	}
	for i := 0; i < n; i++ {
		f := runtime.FuncForPC(pc[i])
		file, line := f.FileLine(pc[i])
		formattedLine = fmt.Sprintf("%s:%d %s\n", file, line, f.Name())
		if _, err = lockfile.WriteString(formattedLine); err != nil {
			fmt.Fprintf(os.Stderr, "internal error: could not write trace callers to tmp file: %v", err)
			return
		}
	}
}

func GetLockStart() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

// MaybeSaveLockTime allows to save lock times when this overpass the threshold
// defined by through the SNAPD_STATE_LOCK_THRESHOLD_MS environment settings.
func MaybeSaveLockTime(lockStart int64) {
	lockEnd := time.Now().UnixNano() / int64(time.Millisecond)

	if !GetenvBool("SNAPPY_TESTING") {
		return
	}
	threshold := GetenvInt64("SNAPD_STATE_LOCK_THRESHOLD_MS")
	if threshold <= 0 {
		return
	}

	elapsedMilliseconds := lockEnd - lockStart
	if elapsedMilliseconds > threshold {
		formattedLine := fmt.Sprintf("Elapsed Time: %d milliseconds", elapsedMilliseconds)
		traceCallers(formattedLine)
	}
}
