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
	"log"
	"log/syslog"
	"os"
	"sync"
)

type Logger interface {
	// Notice is for messages that the user should see
	Notice(msg string)
	// Debug is for messages that the user should be able to find if they're debugging something
	Debug(msg string)
}

const (
	DefaultFlags   = log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile
	SyslogFlags    = log.Lshortfile
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

// Panic notifies the user and then panics
func Panic(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.Notice("PANIC " + msg)
	panic(msg)
}

// Notice notifies the user of something
func Notice(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.Notice(msg)
}

// Debug records something in the debug log
func Debug(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)

	lock.Lock()
	defer lock.Unlock()

	logger.Debug(msg)
}

// Set the global logger to the given one
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
	l.sys.Output(3, "DEBUG: "+msg)
}

// Notice alerts the user about something, as well as putting it syslog
func (l *ConsoleLog) Notice(msg string) {
	l.sys.Output(3, msg)
	l.log.Output(3, msg)
}

// NewConsoleLog creates a ConsoleLog with a log.Logger using the given
// io.Writer and flag, and a syslog.Writer.
func NewConsoleLog(w io.Writer, flag int) (*ConsoleLog, error) {
	sys, err := syslog.NewLogger(SyslogPriority, SyslogFlags)
	if err != nil {
		return nil, err
	}

	return &ConsoleLog{
		log: log.New(w, "", flag),
		sys: sys,
	}, nil
}

func SimpleSetup() error {
	l, err := NewConsoleLog(os.Stderr, DefaultFlags)
	if err != nil {
		return err
	}
	SetLogger(l)

	return nil
}
