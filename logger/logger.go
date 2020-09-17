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

package logger

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

// A Logger is a fairly minimal logging tool.
type Logger interface {
	// Notice is for messages that the user should see
	Notice(msg string)
	// Debug is for messages that the user should be able to find if they're debugging something
	Debug(msg string)
}

const (
	// DefaultFlags are passed to the default console log.Logger
	DefaultFlags = log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile
)

type nullLogger struct{}

func (nullLogger) Notice(string) {}
func (nullLogger) Debug(string)  {}

// NullLogger is a logger that does nothing
var NullLogger = nullLogger{}

var (
	logger Logger = NullLogger
	lock   sync.Mutex
)

// Panicf notifies the user and then panics
func Panicf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.Notice("PANIC " + msg)
	panic(msg)
}

// Noticef notifies the user of something
func Noticef(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.Notice(msg)
}

// Debugf records something in the debug log
func Debugf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.Debug(msg)
}

// MockLogger replaces the exiting logger with a buffer and returns
// the log buffer and a restore function.
func MockLogger() (buf *bytes.Buffer, restore func()) {
	buf = &bytes.Buffer{}
	oldLogger := logger
	l, err := New(buf, DefaultFlags)
	if err != nil {
		panic(err)
	}
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
}

// Debug only prints if SNAPD_DEBUG is set
func (l *Log) Debug(msg string) {
	if l.debug || osutil.GetenvBool("SNAPD_DEBUG") {
		l.log.Output(3, "DEBUG: "+msg)
	}
}

// Notice alerts the user about something, as well as putting it syslog
func (l *Log) Notice(msg string) {
	l.log.Output(3, msg)
}

// New creates a log.Logger using the given io.Writer and flag.
func New(w io.Writer, flag int) (Logger, error) {
	logger := &Log{
		log:   log.New(w, "", flag),
		debug: debugEnabledOnKernelCmdline(),
	}
	return logger, nil
}

// SimpleSetup creates the default (console) logger
func SimpleSetup() error {
	flags := log.Lshortfile
	if term := os.Getenv("TERM"); term != "" {
		// snapd is probably not running under systemd
		flags = DefaultFlags
	}
	l, err := New(os.Stderr, flags)
	if err == nil {
		SetLogger(l)
	}
	return err
}

var procCmdline = "/proc/cmdline"

// TODO: consider generalizing this to snapdenv and having it used by
// other places that consider SNAPD_DEBUG
func debugEnabledOnKernelCmdline() bool {
	buf, err := ioutil.ReadFile(procCmdline)
	if err != nil {
		return false
	}
	l := strings.Split(string(buf), " ")
	return strutil.ListContains(l, "snapd.debug=1")
}
