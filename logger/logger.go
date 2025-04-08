// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014,2015,2017 Canonical Ltd
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

// The logger package implements logging facilities for snapd.
// When built with the structuredlogging build tag, it offers the ability
// to use structured JSON for log entries and to turn on trace logging.
// To activate JSON logging, the SNAPD_JSON_LOGGING environment variable
// should be set at the time of logger creation. Trace logging can be
// activated by setting the SNAPD_TRACE env variable.
//
// When built without the structuredlogging build tag, the logger package
// offers only the simple logger and will not activate trace logging even
// if the SNAPD_TRACE env variable is set.
package logger

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/kcmdline"
)

// A Logger is a fairly minimal logging tool.
type Logger interface {
	// Notice is for messages that the user should see
	Notice(msg string)
	// Debug is for messages that the user should be able to find if they're debugging something
	Debug(msg string)
	// NoGuardDebug is for messages that we always want to print (e.g., configurations
	// were checked by the caller, etc)
	NoGuardDebug(msg string)
	// Trace is for messages useful for tracing execution
	Trace(msg string, attrs ...any)
}

const (
	// DefaultFlags are passed to the default console log.Logger
	DefaultFlags = log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile
)

type nullLogger struct{}

func (nullLogger) Notice(string)        {}
func (nullLogger) Debug(string)         {}
func (nullLogger) NoGuardDebug(string)  {}
func (nullLogger) Trace(string, ...any) {}

// NullLogger is a logger that does nothing
var NullLogger = nullLogger{}

var (
	logger Logger = NullLogger
	lock   sync.Mutex
)

// Panicf notifies the user and then panics
func Panicf(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.Notice("PANIC " + msg)
	panic(msg)
}

// Noticef notifies the user of something
func Noticef(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	lock.Lock()
	defer lock.Unlock()

	logger.Notice(msg)
}

// Notice notifies the user of something
func Notice(msg string) {
	lock.Lock()
	defer lock.Unlock()

	logger.Notice(msg)
}

// Debugf records something in the debug log
func Debugf(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	lock.Lock()
	defer lock.Unlock()

	logger.Debug(msg)
}

// Debug records something in the debug log
func Debug(msg string) {
	lock.Lock()
	defer lock.Unlock()

	logger.Debug(msg)
}

// Trace records something in the trace log
func Trace(msg string, attrs ...any) {
	lock.Lock()
	defer lock.Unlock()

	logger.Trace(msg, attrs...)
}

// NoGuardDebugf records something in the debug log
func NoGuardDebugf(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.NoGuardDebug(msg)
}

// MockLogger replaces the existing logger with a buffer and returns
// the log buffer and a restore function.
func MockLogger() (buf *bytes.Buffer, restore func()) {
	return mockLogger(&LoggerOptions{})
}

// MockDebugLogger replaces the existing logger with a buffer and returns
// the log buffer and a restore function. The logger records debug messages.
func MockDebugLogger() (buf *bytes.Buffer, restore func()) {
	return mockLogger(&LoggerOptions{ForceDebug: true})
}

func mockLogger(opts *LoggerOptions) (buf *bytes.Buffer, restore func()) {
	buf = &bytes.Buffer{}
	oldLogger := logger
	l := New(buf, DefaultFlags, opts)
	SetLogger(l)
	return buf, func() {
		SetLogger(oldLogger)
	}
}

// WithLoggerLock invokes f with the global logger lock, useful for
// tests involving goroutines with MockLogger.
func WithLoggerLock(f func()) {
	lock.Lock()
	defer lock.Unlock()

	f()
}

// SetLogger sets the global logger to the given one
func SetLogger(l Logger) {
	lock.Lock()
	defer lock.Unlock()

	logger = l
}

type Log struct {
	log *log.Logger

	debug bool
	quiet bool
	flags int
}

func (l *Log) debugEnabled() bool {
	return l.debug || osutil.GetenvBool("SNAPD_DEBUG")
}

// Debug only prints if SNAPD_DEBUG is set
func (l *Log) Debug(msg string) {
	if l.debugEnabled() {
		// this frame + single package level API func() + actual caller
		calldepth := 1 + 1 + 1
		l.log.Output(calldepth, "DEBUG: "+msg)
	}
}

// Notice alerts the user about something, as well as putting in syslog
func (l *Log) Notice(msg string) {
	if !l.quiet || l.debugEnabled() {
		// this frame + single package level API func() + actual caller
		calldepth := 1 + 1 + 1
		l.log.Output(calldepth, msg)
	}
}

// Trace only prints if SNAPD_TRACE is set and the structured logger is used
func (l *Log) Trace(string, ...any) {}

// NoGuardDebug always prints the message, w/o gating it based on environment
// variables or other configurations.
func (l *Log) NoGuardDebug(msg string) {
	// this frame + single package level API func() + actual caller
	calldepth := 1 + 1 + 1
	l.log.Output(calldepth, "DEBUG: "+msg)
}

func newLog(w io.Writer, flag int, opts *LoggerOptions) Logger {
	logger := &Log{
		log:   log.New(w, "", flag),
		debug: opts.ForceDebug || debugEnabledOnKernelCmdline(),
		flags: flag,
	}
	return logger
}

type LoggerOptions struct {
	// ForceDebug can be set if we want debug traces even if not directly
	// enabled by environment or kernel command line.
	ForceDebug bool
}

func buildFlags() int {
	flags := log.Lshortfile
	if term := os.Getenv("TERM"); term != "" {
		// snapd is probably not running under systemd
		flags = DefaultFlags
	}
	return flags
}

// SimpleSetup creates the default (console) logger
func SimpleSetup(opts *LoggerOptions) {
	flags := buildFlags()
	l := New(os.Stderr, flags, opts)
	SetLogger(l)
}

// BootSetup creates a logger meant to be used when running from
// initramfs, where we want to consider the quiet kernel option.
func BootSetup() error {
	flags := buildFlags()
	m, _ := kcmdline.KeyValues("quiet")
	_, quiet := m["quiet"]
	logger := &Log{
		log:   log.New(os.Stderr, "", flags),
		debug: debugEnabledOnKernelCmdline(),
		quiet: quiet,
		flags: flags,
	}
	SetLogger(logger)

	return nil
}

// used to force testing of the kernel command line parsing
var procCmdlineUseDefaultMockInTests = true

// TODO: consider generalizing this to snapdenv and having it used by
// other places that consider SNAPD_DEBUG
func debugEnabledOnKernelCmdline() bool {
	// if this is called during tests, always ignore it so we don't have to mock
	// the /proc/cmdline for every test that ends up using a logger
	if osutil.IsTestBinary() && procCmdlineUseDefaultMockInTests {
		return false
	}
	m, _ := kcmdline.KeyValues("snapd.debug")
	return m["snapd.debug"] == "1"
}

var timeNow = time.Now

// StartupStageTimestamp produce snap startup timings message.
func StartupStageTimestamp(stage string) {
	now := timeNow()
	Debugf(`-- snap startup {"stage":"%s", "time":"%v.%06d"}`,
		stage, now.Unix(), (now.UnixNano()/1e3)%1e6)
}
