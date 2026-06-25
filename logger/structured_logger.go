// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build structuredlogging

/*
 * Copyright (C) 2025 Canonical Ltd
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

package logger

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/kcmdline"
)

type StructuredLog struct {
	log      *slog.Logger
	debug    bool
	trace    bool
	quiet    bool
	flags    int
	seenMu   sync.RWMutex
	seenLogs map[string]bool
}

const (
	levelTrace  = slog.Level(-8)
	levelNotice = slog.Level(2)
)

var levelNames = map[slog.Level]string{
	levelTrace:  "TRACE",
	levelNotice: "NOTICE",
}

func (l *StructuredLog) debugEnabled() bool {
	return l.debug || osutil.GetenvBool("SNAPD_DEBUG") || l.traceEnabled()
}

// Debug only prints if SNAPD_DEBUG or SNAPD_TRACE is set
func (l *StructuredLog) Debug(msg string) {
	if l.debugEnabled() {
		var pcs [1]uintptr
		runtime.Callers(3, pcs[:])
		r := slog.NewRecord(time.Now(), slog.LevelDebug, msg, pcs[0])
		l.log.Handler().Handle(context.Background(), r)
	}
}

// Notice alerts the user about something, as well as putting in syslog
func (l *StructuredLog) Notice(msg string) {
	if !l.quiet {
		var pcs [1]uintptr
		runtime.Callers(3, pcs[:])
		r := slog.NewRecord(time.Now(), levelNotice, msg, pcs[0])
		l.log.Handler().Handle(context.Background(), r)
	}
}

// NoGuardDebug always prints the message, w/o gating it based on environment
// variables or other configurations.
func (l *StructuredLog) NoGuardDebug(msg string) {
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), slog.LevelDebug, msg, pcs[0])
	l.log.Handler().Handle(context.Background(), r)
}

func (l *StructuredLog) traceEnabled() bool {
	if l.trace {
		return true
	}
	if osutil.GetenvBool("SNAPD_TRACE") {
		l.trace = true
		return true
	}
	return false
}

// logKeyHash generates a unique hash for a message and its attributes
// to use for deduplication purposes.
func (l *StructuredLog) logKeyHash(msg string, attrs ...any) string {
	h := md5.New()
	fmt.Fprintf(h, "%s", msg)
	for _, attr := range attrs {
		fmt.Fprintf(h, "%v", attr)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// alreadyLogged checks if the given message+attrs combination has been logged before
func (l *StructuredLog) alreadyLogged(msg string, attrs ...any) bool {
	key := l.logKeyHash(msg, attrs...)
	l.seenMu.RLock()
	defer l.seenMu.RUnlock()
	return l.seenLogs[key]
}

// markLogged records that a message+attrs combination has been logged
func (l *StructuredLog) markLogged(msg string, attrs ...any) {
	key := l.logKeyHash(msg, attrs...)
	l.seenMu.Lock()
	defer l.seenMu.Unlock()
	l.seenLogs[key] = true
}

func (l *StructuredLog) Trace(msg string, attrs ...any) {
	if l.traceEnabled() && !l.alreadyLogged(msg, attrs...) {
		var pcs [1]uintptr
		runtime.Callers(3, pcs[:])
		r := slog.NewRecord(time.Now(), levelTrace, msg, pcs[0])
		r.Add(attrs...)
		l.log.Handler().Handle(context.Background(), r)
		l.markLogged(msg, attrs...)
	}
}

// // New creates a log.Logger using the given io.Writer and flag, using the
// // options from opts.
func New(w io.Writer, flag int, opts *LoggerOptions) Logger {
	if opts == nil {
		opts = &LoggerOptions{}
	}
	if !osutil.GetenvBool("SNAPD_JSON_LOGGING") && !jsonLoggingEnabledOnKernelCmdline() {
		return newLog(w, flag, opts)
	}
	options := &slog.HandlerOptions{
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// The simple logger uses the flag to determine what gets
			// added to the logs. slog uses attributes. To keep the
			// same functionality as with the simple log, here we check
			// the flags if the timestamp should be removed and if the
			// filename only should be considered instead of full path.
			if a.Key == slog.TimeKey && (flag&log.Ldate) != log.Ldate {
				// Remove timestamp
				return slog.Attr{}
			}
			if a.Key == slog.SourceKey && (flag&log.Lshortfile) == log.Lshortfile {
				// Remove all but the file name of the source file
				source, ok := a.Value.Any().(*slog.Source)
				if !ok {
					return a
				}
				if source != nil {
					source.File = filepath.Base(source.File)
				}
				return a
			}
			if a.Key == slog.LevelKey {
				// Add TRACE and NOTICE level names
				level, ok := a.Value.Any().(slog.Level)
				if !ok {
					return a
				}
				levelLabel, exists := levelNames[level]
				if !exists {
					levelLabel = level.String()
				}
				a.Value = slog.StringValue(levelLabel)
			}
			return a
		},
	}
	logger := &StructuredLog{
		log:      slog.New(slog.NewJSONHandler(w, options)),
		debug:    opts.ForceDebug || debugEnabledOnKernelCmdline(),
		flags:    flag,
		trace:    traceEnabledOnKernelCmdline(),
		seenLogs: make(map[string]bool),
	}
	return logger
}

func traceEnabledOnKernelCmdline() bool {
	// if this is called during tests, always ignore it so we don't have to mock
	// the /proc/cmdline for every test that ends up using a logger
	if osutil.IsTestBinary() && procCmdlineUseDefaultMockInTests {
		return false
	}
	m, _ := kcmdline.KeyValues("tag.features")
	return m["tag.features"] == "1"
}

func jsonLoggingEnabledOnKernelCmdline() bool {
	// if this is called during tests, always ignore it so we don't have to mock
	// the /proc/cmdline for every test that ends up using a logger
	if osutil.IsTestBinary() && procCmdlineUseDefaultMockInTests {
		return false
	}
	m, _ := kcmdline.KeyValues("tag.features")
	return m["tag.features"] == "1"
}
