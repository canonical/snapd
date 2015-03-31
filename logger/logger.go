package logger

import (
	"fmt"
	"log/syslog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/juju/loggo"
)

// Name used in the prefix for all logged messages
const LoggerName = "snappy"

// logWriterInterface allows the tests to replace the syslog
// implementation.
type logWriterInterface interface {
	// syslog.Writer
	Debug(m string) error
	Info(m string) error
	Warning(m string) error
	Err(m string) error
	Crit(m string) error
}

// LogWriter object that handles writing log entries
type LogWriter struct {
	systemLog logWriterInterface
}

// Used to ensure that only a single connection to syslog is created
var once sync.Once

// A single connection to the system logger
var syslogConnection logWriterInterface

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

	// write the log record.
	//
	// Note that with loggo, the actual logging functions never
	// return errors, so although these syslog functions may fail,
	// there's not much we can do about it.
	f(record)

	if level >= loggo.ERROR {
		l.logStacktrace(level, name, filename, line, timestamp, f)
	}
}

func (l *LogWriter) logStacktrace(level loggo.Level, name string, filename string, line int, timestamp time.Time, f func(string) error) {
	stack := debug.Stack()

	str := "Stack trace:"
	record := l.Format(level, name, filename, line, timestamp, str)
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
	if level < loggo.ERROR {
		// keep it relatively succinct for low priority messages
		return fmt.Sprintf("%s:%s:%s", level, module, message)
	}

	return fmt.Sprintf("%s:%s:%s:%d:%s", level, module, filename, line, message)
}

// A variable to make testing easier
var getSyslog = func(priority syslog.Priority, tag string) (w logWriterInterface, err error) {
	return syslog.New(syslog.LOG_NOTICE|syslog.LOG_LOCAL0, LoggerName)
}

// newLogWriter creates a new LogWriter, ensuring that only a single
// connection to the system logger is created.
func newLogWriter() (l *LogWriter, err error) {

	l = new(LogWriter)

	once.Do(func() {

		// Note that the log level here is just the default - Write()
		// will alter it as needed.
		syslogConnection, err = getSyslog(syslog.LOG_NOTICE|syslog.LOG_LOCAL0, LoggerName)
	})

	if err != nil {
		return nil, err
	}

	l.systemLog = syslogConnection

	return l, nil
}

// ActivateLogger handles creating, configuring and enabling a new syslog logger.
func ActivateLogger() (err error) {
	// Remove any existing loggers of the type we care about.
	//
	// No check on the success of these operations is performed since the
	// loggo API does not allow the existing loggers to be iterated so we
	// cannot know if there is already a syslog writer registered (for
	// example if newLogWriter() gets called multiple times).
	_, _, _ = loggo.RemoveWriter("default")
	_, _, _ = loggo.RemoveWriter("syslog")

	writer, err := newLogWriter()
	if err != nil {
		return err
	}

	// activate our syslog logger
	err = loggo.RegisterWriter("syslog", writer, loggo.TRACE)
	if err != nil {
		return err
	}

	logger := loggo.GetLogger(LoggerName)

	// ensure that all log messages are output
	logger.SetLogLevel(loggo.TRACE)

	return nil
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
