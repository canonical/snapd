package logger

import (
	"bytes"
	"errors"
	"fmt"
	"log/syslog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/juju/loggo"
	. "launchpad.net/gocheck"
)

func Test(t *testing.T) { TestingT(t) }

type LoggerTestSuite struct {
}

var _ = Suite(&LoggerTestSuite{})

type MockLogWriter struct {
	buf bytes.Buffer
}

var mockWriter *MockLogWriter

func mockGetSyslog(priority syslog.Priority, tag string) (w logWriterInterface, err error) {
	mockWriter = &MockLogWriter{}
	return mockWriter, nil
}

func readLines() (lines []string) {
	lines = strings.Split(string(mockWriter.buf.Bytes()), "\n")

	// remove the last line if empty due to split.
	length := len(lines)
	last := lines[length-1]
	if last == "" {
		lines = lines[:length-1]
	}
	// clear the buffer to avoid contents accumulating indefinitely
	mockWriter.buf.Reset()
	return lines
}

func (ts *LoggerTestSuite) SetUpTest(c *C) {
	getSyslog = mockGetSyslog
}

func (w *MockLogWriter) Debug(m string) error {
	_, err := w.buf.Write([]byte(fmt.Sprintf("DEBUG: %s\n", m)))
	return err
}

func (w *MockLogWriter) Info(m string) error {
	_, err := w.buf.Write([]byte(fmt.Sprintf("INFO: %s\n", m)))
	return err
}

func (w *MockLogWriter) Warning(m string) error {
	_, err := w.buf.Write([]byte(fmt.Sprintf("WARNING: %s\n", m)))
	return err
}

func (w *MockLogWriter) Err(m string) error {
	_, err := w.buf.Write([]byte(fmt.Sprintf("ERROR: %s\n", m)))
	return err
}

func (w *MockLogWriter) Crit(m string) error {
	_, err := w.buf.Write([]byte(fmt.Sprintf("CRITICAL: %s\n", m)))
	return err
}

// Search for value in array and return true if found
func sliceContainsString(array []string, value string) bool {
	str := string(strings.Join(array, ""))

	return strings.Contains(str, value)
}

// Return true if array contains the pattern regex.
func sliceContainsRegex(array []string, regex string) bool {
	str := string(strings.Join(array, ""))

	pattern := regexp.MustCompile(regex)

	matches := pattern.FindAllStringSubmatch(str, -1)

	return matches != nil
}

func (ts *LoggerTestSuite) TestNewLogWriter(c *C) {
	var w, w2 *LogWriter
	var err error

	w, err = newLogWriter()
	c.Assert(err, IsNil)
	c.Assert(w, Not(IsNil))
	c.Assert(w.systemLog, Not(IsNil))

	w2, err = newLogWriter()
	c.Assert(err, IsNil)
	c.Assert(w2, Not(IsNil))
	c.Assert(w2.systemLog, Not(IsNil))

	// There should be a single shared syslog connection, hence the
	// systemLog objects should be identical.
	c.Assert(w.systemLog, Equals, w2.systemLog)
	c.Assert(w.systemLog, DeepEquals, w2.systemLog)
}

func (ts *LoggerTestSuite) TestWrite(c *C) {
	w, err := newLogWriter()
	c.Assert(err, IsNil)
	c.Assert(w, Not(IsNil))
	c.Assert(w.systemLog, Not(IsNil))

	t := time.Now()
	strTime := fmt.Sprintf("%s", t)

	w.Write(loggo.DEBUG, "module", "filename", 1234, t, "a message")
	lines := readLines()
	c.Assert(len(lines), Equals, 1)

	c.Assert(sliceContainsString(lines, "module"), Equals, true)
	c.Assert(sliceContainsString(lines, "filename"), Equals, true)
	c.Assert(sliceContainsString(lines, "1234"), Equals, true)

	// We discard the timestamp as syslog adds that itself
	c.Assert(sliceContainsString(lines, strTime), Equals, false)

	c.Assert(sliceContainsString(lines, "a message"), Equals, true)
}

func (ts *LoggerTestSuite) TestFormat(c *C) {
	w, err := newLogWriter()
	c.Assert(err, IsNil)
	c.Assert(w, Not(IsNil))
	c.Assert(w.systemLog, Not(IsNil))

	out := w.Format(loggo.ERROR, "module", "filename", 1234, time.Now(), "a message")

	expected := fmt.Sprintf("%s:%s:%s:%d:%s", "ERROR", "module", "filename", 1234, "a message")

	c.Assert(out, Equals, expected)
}

func (ts *LoggerTestSuite) TestLogStackTrace(c *C) {
	var output []string

	w, err := newLogWriter()
	c.Assert(err, IsNil)
	c.Assert(w, Not(IsNil))

	f := func(s string) error {
		output = append(output, s)
		return nil
	}

	t := time.Now()
	strTime := fmt.Sprintf("%s", t)

	w.logStacktrace(loggo.DEBUG, "name", "filename", 9876, t, f)

	c.Assert(sliceContainsString(output, "Stack trace"), Equals, true)
	c.Assert(sliceContainsString(output, "name"), Equals, true)
	c.Assert(sliceContainsString(output, "filename"), Equals, true)
	c.Assert(sliceContainsString(output, "9876"), Equals, true)

	// We discard the timestamp as syslog adds that itself
	c.Assert(sliceContainsString(output, strTime), Equals, false)
}

func (ts *LoggerTestSuite) checkLogLevel(c *C, level, msg string) {
	err := ActivateLogger()
	c.Assert(err, IsNil)

	expectBacktrace := (level == "ERROR" || level == "CRITICAL")

	logger := loggo.GetLogger("snappy")
	c.Assert(logger, Not(IsNil))

	switch level {
	case "DEBUG":
		c.Assert(logger.IsDebugEnabled(), Equals, true)
		logger.Debugf(msg)

	case "INFO":
		c.Assert(logger.IsInfoEnabled(), Equals, true)
		logger.Infof(msg)

	case "ERROR":
		c.Assert(logger.IsErrorEnabled(), Equals, true)
		logger.Errorf(msg)

	case "CRITICAL":
		// loggo doesn't provide a IsCriticalEnabled()
		c.Assert(logger.IsErrorEnabled(), Equals, true)
		logger.Criticalf(msg)
	}

	lines := readLines()

	if expectBacktrace {
		c.Assert(len(lines) > 1, Equals, true)
	} else {
		c.Assert(len(lines), Equals, 1)
	}

	needle := fmt.Sprintf("%s.*%s", level, msg)
	c.Assert(sliceContainsRegex(lines, needle), Equals, true)

	c.Assert(sliceContainsString(lines, "Stack trace"), Equals, expectBacktrace)
}

func (ts *LoggerTestSuite) TestLogLevels(c *C) {
	msg := "an error message"
	levels := []string{"DEBUG", "INFO", "ERROR", "CRITICAL"}

	for _, level := range levels {
		ts.checkLogLevel(c, level, msg)
	}
}

func (ts *LoggerTestSuite) TestLogError(c *C) {
	level := "ERROR"
	msg := "I am an error"

	err := ActivateLogger()
	c.Assert(err, IsNil)

	result := LogError(nil)
	c.Assert(result, IsNil)

	err = errors.New(msg)
	c.Assert(err, Not(IsNil))

	// We expect to get back exactly what was passsed...
	result = LogError(err)
	c.Assert(result, DeepEquals, err)

	// ... but also to have the error logged
	ts.checkLogLevel(c, level, msg)
}

func (ts *LoggerTestSuite) TestLogAndPanic(c *C) {
	level := "CRITICAL"
	msg := "I am a fatal error"

	err := ActivateLogger()
	c.Assert(err, IsNil)

	err = errors.New(msg)

	// expect a panic...
	c.Assert(func() { LogAndPanic(err) }, Panics, err)

	// ... and a log entry
	ts.checkLogLevel(c, level, msg)
}
