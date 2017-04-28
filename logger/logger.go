// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"os"
	"sync"

	"github.com/snapcore/snapd/osutil"
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
	// SyslogFlags are passed to the default syslog log.Logger
	SyslogFlags = log.Lshortfile
	// SyslogPriority for the default syslog log.Logger
	SyslogPriority = syslog.LOG_DEBUG | syslog.LOG_USER
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

// SetLogger sets the global logger to the given one
func SetLogger(l Logger) {
	lock.Lock()
	defer lock.Unlock()

	logger = l
}

// ConsoleLog sends Notices to a log.Logger and Debugs to syslog
type ConsoleLog struct {
	log *log.Logger
	sys *log.Logger
}

// Debug sends the msg to syslog
func (l *ConsoleLog) Debug(msg string) {
	s := "DEBUG: " + msg
	l.sys.Output(3, s)

	if osutil.GetenvBool("SNAPD_DEBUG") {
		l.log.Output(3, s)
	}
}

// Notice alerts the user about something, as well as putting it syslog
func (l *ConsoleLog) Notice(msg string) {
	l.sys.Output(3, msg)
	l.log.Output(3, msg)
}

// variable to allow mocking the syslog.NewLogger call in the tests
var newSyslog = newSyslogImpl

func newSyslogImpl() (*log.Logger, error) {
	return syslog.NewLogger(SyslogPriority, SyslogFlags)
}

// NewConsoleLog creates a ConsoleLog with a log.Logger using the given
// io.Writer and flag, and a syslog.Writer.
func NewConsoleLog(w io.Writer, flag int) (*ConsoleLog, error) {
	clog := log.New(w, "", flag)

	sys, err := newSyslog()
	if err != nil {
		clog.Output(3, "WARNING: cannot create syslog logger")
		sys = log.New(ioutil.Discard, "", flag)
	}

	return &ConsoleLog{
		log: clog,
		sys: sys,
	}, nil
}

// SimpleSetup creates the default (console) logger
func SimpleSetup() error {
	l, err := NewConsoleLog(os.Stderr, DefaultFlags)
	if err != nil {
		return err
	}
	SetLogger(l)

	return nil
}
