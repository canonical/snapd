package logger

import (
	"fmt"
	"log/syslog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/juju/loggo"
)

// Name used in the prefix for all logged messages
const LoggerName = "snappy"

type LogWriterInterface interface {
	// io.Writer
	Write(p []byte) (n int, err error)

	// syslog.Writer
	Debug(m string) error
	Info(m string) error
	Warning(m string) error
	Err(m string) error
	Crit(m string) error
}

// LogWriter object that handles writing log entries
type LogWriter struct {
	//systemLog *syslog.Writer
	systemLog LogWriterInterface
}

// Write sends the log details specified by the params to the logging
// back-end (in this case syslog).
func (l *LogWriter) Write(level loggo.Level, name string, filename string, line int, timestamp time.Time, message string) {
	var f func(string) error

	record := l.Format(level, name, filename, line, timestamp, message)

	// map log level to syslog priority
	switch level {
	case loggo.DEBUG:
		f = l.systemLog.Debug

	case loggo.INFO:
		f = l.systemLog.Info

	case loggo.WARNING:
		f = l.systemLog.Warning

	case loggo.ERROR:
		f = l.systemLog.Err

	case loggo.CRITICAL:
		f = l.systemLog.Crit
	}

	// write record
	f(record)

	if level < loggo.ERROR {
		return
	}

	// add a stack trace for important messages
	stack := debug.Stack()

	str := "Stack trace:"
	record = l.Format(level, name, filename, line, timestamp, str)
	f(record)

	for _, entry := range strings.Split(string(stack), "\n") {
		if entry == "" {
			continue
		}

		formatted := fmt.Sprintf("  %s", strings.Replace(entry, "\t", "  ", -1))
		record := l.Format(level, name, filename, line, timestamp, formatted)
		f(record)
	}
}

// Format handles how each log entry should appear.
// Note that the timestamp field is not used as syslog adds that for us.
func (l *LogWriter) Format(level loggo.Level, module, filename string, line int, timestamp time.Time, message string) string {
	return fmt.Sprintf("%s:%s:%s:%d:%s", level, module, filename, line, message)
}

// A variable to make testing easier
var getSyslog = func(priority syslog.Priority, tag string) (w LogWriterInterface, err error) {
	return syslog.New(syslog.LOG_NOTICE|syslog.LOG_LOCAL0, LoggerName)
}

// NewLogWriter creates a new LogWriter.
func NewLogWriter() *LogWriter {
	var err error

	l := new(LogWriter)

	// Note that the log level here is just the default - Write()
	// will alter it as needed.
	l.systemLog, err = getSyslog(syslog.LOG_NOTICE|syslog.LOG_LOCAL0, LoggerName)

	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to connect to syslog - persistent logging will be disabled: %s", err)
	}

	// Remove any existing loggers of the type we care about.
	//
	// No check on the success of these operations is performed since the
	// loggo API does not allow the existing loggers to be iterated so we
	// cannot know if there is already a syslog writer registered (for
	// example if NewLogWriter() gets called multiple times).
	_, _, _ = loggo.RemoveWriter("default")
	_, _, _ = loggo.RemoveWriter("syslog")

	// activate our syslog logger
	err = loggo.RegisterWriter("syslog", l, loggo.TRACE)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to register syslog logger: %q\n", err)
	}

	return l
}

// Activate handles creating, configuring and enabling a new syslog logger.
func ActivateLogger() {
	loggo.RegisterWriter("syslog", NewLogWriter(), loggo.TRACE)

	logger := loggo.GetLogger(LoggerName)

	// ensure that all log messages are output
	logger.SetLogLevel(loggo.TRACE)
}

// LogAndPanic logs the specified error, including a backtrace, then calls
// panic().
func LogAndPanic(err error) {
	if err == nil {
		return
	}

	logger := loggo.GetLogger(LoggerName)
	logger.Criticalf(err.Error())
	panic(err)
}

// LogError logs the specified error (if set), then returns it to be dealt with by
// higher-level parts of the system.
func LogError(err error) error {
	if err == nil {
		return nil
	}

	logger := loggo.GetLogger(LoggerName)
	logger.Errorf(err.Error())
	return err
}
