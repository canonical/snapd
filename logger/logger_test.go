package logger

import (
	"bufio"
	"errors"
	"fmt"
	"log/syslog"
	"os"
	"path/filepath"
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
	logfileName string
	file        *os.File
}

var logfileName string

func mockGetSyslog(priority syslog.Priority, tag string) (writer LogWriterInterface, err error) {
	w := new(MockLogWriter)

	w.logfileName = logfileName

	flags := (os.O_RDWR | os.O_CREATE | os.O_APPEND)

	w.file, err = os.OpenFile(w.logfileName, flags, 0640)
	if err != nil {
		return nil, err
	}

	return w, nil
}

func readLines(path string) (lines []string, err error) {

	file, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

func (ts *LoggerTestSuite) SetUpTest(c *C) {
	dir := c.MkDir()

	logfileName = filepath.Join(dir, "file.log")
	getSyslog = mockGetSyslog
}

func (ts *LoggerTestSuite) TearDownTest(c *C) {
	os.Remove(logfileName)
}

func (w *MockLogWriter) write(m string) error {
	_, err := w.file.Write([]byte(m))
	return err
}

func (w *MockLogWriter) Write(bytes []byte) (n int, err error) {
	return n, w.write(string(bytes))
}

func (w *MockLogWriter) Debug(m string) error {
	return w.write(fmt.Sprintf("DEBUG: %s\n", m))
}

func (w *MockLogWriter) Info(m string) error {
	return w.write(fmt.Sprintf("INFO: %s\n", m))
}

func (w *MockLogWriter) Warning(m string) error {
	return w.write(fmt.Sprintf("WARNING: %s\n", m))
}

func (w *MockLogWriter) Err(m string) error {
	return w.write(fmt.Sprintf("ERROR: %s\n", m))
}

func (w *MockLogWriter) Crit(m string) error {
	return w.write(fmt.Sprintf("CRITICAL: %s\n", m))
}

// Search for value in array and return true if found
func sliceContainsString(array []string, value string) bool {
	str := string(strings.Join(array, ""))

	return strings.Contains(str, value)
}

// Return true if array contains the patter regex.
func sliceContainsRegex(array []string, regex string) bool {
	str := string(strings.Join(array, ""))

	pattern := regexp.MustCompile(regex)

	matches := pattern.FindAllStringSubmatch(str, -1)

	return matches != nil
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)

	if err != nil {
		return -1
	}

	return st.Size()
}

func (ts *LoggerTestSuite) TestNewLogWriter(c *C) {
	w := NewLogWriter()
	c.Assert(w, Not(IsNil))
	c.Assert(w.systemLog, Not(IsNil))
}

func (ts *LoggerTestSuite) TestWrite(c *C) {
	w := NewLogWriter()
	c.Assert(w, Not(IsNil))
	c.Assert(w.systemLog, Not(IsNil))

	t := time.Now()
	strTime := fmt.Sprintf("%s", t)

	w.Write(loggo.DEBUG, "module", "filename", 1234, t, "a message")
	lines, err := readLines(logfileName)
	c.Assert(err, IsNil)

	c.Assert(sliceContainsString(lines, "module"), Equals, true)
	c.Assert(sliceContainsString(lines, "filename"), Equals, true)
	c.Assert(sliceContainsString(lines, "1234"), Equals, true)

	// We discard the timestamp as syslog adds that itself
	c.Assert(sliceContainsString(lines, strTime), Equals, false)

	c.Assert(sliceContainsString(lines, "a message"), Equals, true)
}

func (ts *LoggerTestSuite) TestFormat(c *C) {
	w := NewLogWriter()
	c.Assert(w, Not(IsNil))
	c.Assert(w.systemLog, Not(IsNil))

	out := w.Format(loggo.ERROR, "module", "filename", 1234, time.Now(), "a message")

	expected := fmt.Sprintf("%s:%s:%s:%d:%s", "ERROR", "module", "filename", 1234, "a message")

	c.Assert(out, Equals, expected)
}

func (ts *LoggerTestSuite) TestDebugLogLevel(c *C) {
	level := "DEBUG"
	msg := "a debug message"

	ActivateLogger()

	logger := loggo.GetLogger("snappy")
	c.Assert(logger, Not(IsNil))

	c.Assert(logger.IsDebugEnabled(), Equals, true)

	logger.Debugf(msg)

	lines, err := readLines(logfileName)
	c.Assert(err, IsNil)

	needle := fmt.Sprintf("%s.*%s", level, msg)
	c.Assert(sliceContainsRegex(lines, needle), Equals, true)
	c.Assert(sliceContainsRegex(lines, "Stack trace"), Equals, false)
}

func (ts *LoggerTestSuite) TestInfoLogLevel(c *C) {
	level := "INFO"
	msg := "an info message"

	ActivateLogger()

	logger := loggo.GetLogger("snappy")
	c.Assert(logger, Not(IsNil))

	c.Assert(logger.IsInfoEnabled(), Equals, true)

	logger.Infof(msg)

	lines, err := readLines(logfileName)
	c.Assert(err, IsNil)

	needle := fmt.Sprintf("%s.*%s", level, msg)
	c.Assert(sliceContainsRegex(lines, needle), Equals, true)
	c.Assert(sliceContainsRegex(lines, "Stack trace"), Equals, false)
}

func (ts *LoggerTestSuite) TestErrorLogLevel(c *C) {
	level := "ERROR"
	msg := "an error message"

	ActivateLogger()

	logger := loggo.GetLogger("snappy")
	c.Assert(logger, Not(IsNil))

	c.Assert(logger.IsErrorEnabled(), Equals, true)

	logger.Errorf(msg)

	lines, err := readLines(logfileName)
	c.Assert(err, IsNil)

	needle := fmt.Sprintf("%s.*%s", level, msg)
	c.Assert(sliceContainsRegex(lines, needle), Equals, true)
	c.Assert(sliceContainsRegex(lines, "Stack trace"), Equals, true)
}

func (ts *LoggerTestSuite) TestCriticalLogLevel(c *C) {
	level := "CRITICAL"
	msg := "a critical message"

	ActivateLogger()

	logger := loggo.GetLogger("snappy")
	c.Assert(logger, Not(IsNil))

	// loggo doesn't provide a IsCriticalEnabled()
	c.Assert(logger.IsErrorEnabled(), Equals, true)

	logger.Criticalf(msg)

	lines, err := readLines(logfileName)
	c.Assert(err, IsNil)

	needle := fmt.Sprintf("%s.*%s", level, msg)
	c.Assert(sliceContainsRegex(lines, needle), Equals, true)
	c.Assert(sliceContainsRegex(lines, "Stack trace"), Equals, true)
}

func (ts *LoggerTestSuite) TestLogError(c *C) {
	level := "ERROR"
	msg := "I am an error"

	ActivateLogger()

	result := LogError(nil)
	c.Assert(result, Equals, nil)

	// no log entries, just an empty log file
	c.Assert(fileSize(logfileName), Equals, int64(0))

	err := errors.New(msg)
	c.Assert(err, Not(IsNil))

	// We expect to get back exactly what was passsed...
	result = LogError(err)
	c.Assert(result, DeepEquals, err)

	// ... but also to have the error logged
	lines, err := readLines(logfileName)
	c.Assert(err, IsNil)

	needle := fmt.Sprintf("%s.*%s", level, msg)
	c.Assert(sliceContainsRegex(lines, needle), Equals, true)
	c.Assert(sliceContainsRegex(lines, "Stack trace"), Equals, true)
}

func (ts *LoggerTestSuite) TestLogAndPanic(c *C) {
	level := "CRITICAL"
	msg := "I am a fatal error"

	ActivateLogger()

	err := errors.New(msg)

	// expect a panic...
	c.Assert(func() { LogAndPanic(err) }, Panics, err)

	// ... and a log entry
	lines, err := readLines(logfileName)
	c.Assert(err, IsNil)

	needle := fmt.Sprintf("%s.*%s", level, msg)
	c.Assert(sliceContainsRegex(lines, needle), Equals, true)
	c.Assert(sliceContainsRegex(lines, "Stack trace"), Equals, true)
}
