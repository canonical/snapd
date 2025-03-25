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
	"io"
	"log"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"

	"github.com/snapcore/snapd/osutil"
)

type StructuredLog struct {
	log   *slog.Logger
	debug bool
	quiet bool
	flags int
}

const (
	LevelTrace  = slog.Level(-8)
	LevelNotice = slog.Level(2)
)

var LevelNames = map[slog.Leveler]string{
	LevelTrace:  "TRACE",
	LevelNotice: "NOTICE",
}

func (l *StructuredLog) debugEnabled() bool {
	return l.debug || osutil.GetenvBool("SNAPD_DEBUG") || osutil.GetenvBool("SNAPD_TRACE")
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
		r := slog.NewRecord(time.Now(), LevelNotice, msg, pcs[0])
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

// Trace only prints if SNAPD_TRACE is set
func (l *StructuredLog) Trace(msg string, attrs ...any) {
	if osutil.GetenvBool("SNAPD_TRACE") {
		var pcs [1]uintptr
		runtime.Callers(3, pcs[:])
		r := slog.NewRecord(time.Now(), LevelTrace, msg, pcs[0])
		r.Add(attrs...)
		l.log.Handler().Handle(context.Background(), r)
	}
}

// // New creates a log.Logger using the given io.Writer and flag, using the
// // options from opts.
func New(w io.Writer, flag int, opts *LoggerOptions) (Logger, error) {
	if opts == nil {
		opts = &LoggerOptions{}
	}
	if !osutil.GetenvBool("SNAPD_JSON_LOGGING") {
		logger := &Log{
			log:   log.New(w, "", flag),
			debug: opts.ForceDebug || debugEnabledOnKernelCmdline(),
			flags: flag,
		}
		return logger, nil
	}
	options := &slog.HandlerOptions{
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && (flag&log.Ldate) != log.Ldate {
				// Remove timestamp
				return slog.Attr{}
			}
			if a.Key == slog.SourceKey && (flag&log.Lshortfile) == log.Lshortfile {
				// Remove all but the file name of the source file
				source, _ := a.Value.Any().(*slog.Source)
				if source != nil {
					source.File = filepath.Base(source.File)
				}
				return a
			}
			if a.Key == slog.LevelKey {
				// Add TRACE and NOTICE level names
				level := a.Value.Any().(slog.Level)
				levelLabel, exists := LevelNames[level]
				if !exists {
					levelLabel = level.String()
				}
				a.Value = slog.StringValue(levelLabel)
			}
			return a
		},
	}
	logger := &StructuredLog{
		log:   slog.New(slog.NewJSONHandler(w, options)),
		debug: opts.ForceDebug || debugEnabledOnKernelCmdline(),
		flags: flag,
	}
	return logger, nil
}
