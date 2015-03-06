package logger

import (
	"fmt"
	"log"
	"log/syslog"
	"os"
	"runtime/debug"
	"strings"
)

// Name appearing in syslog / journald entries.
const syslogName = "SnappyLogger"

// SnappyLogger logging object
type SnappyLogger struct {
	systemLog *syslog.Writer
	disabled  bool
}

// Write implements the io.Writer interface
func (l *SnappyLogger) Write(bytes []byte) (int, error) {
	str := string(bytes)
	length := len(str)

	// Now, log to the systemlog.
	if err := l.systemLog.Info(str); err != nil {
		return 0, err
	}

	return length, nil
}

// New create a new SnappyLogger
func New() *SnappyLogger {
	var err error

	l := new(SnappyLogger)

	l.systemLog, err = syslog.New(syslog.LOG_NOTICE|syslog.LOG_LOCAL0, syslogName)

	// By default, fail hard if any aspect of the logger fails.
	// However, this behaviour can be overriden since if logging
	// fails, it would otherwise not be possible to upgrade a system
	// (for example to fix a logger bug).
	l.disabled = os.Getenv("SNAPPY_LOGGING_DISABLE") != ""

	if err != nil && !l.disabled {
		panic(fmt.Sprintf("error connecting to system logger: %v", err))
	}

	return l
}

// LogError log the specified error (if set), then return it to be dealt with by
// higher-level parts of the system.
func LogError(err error) error {

	if err == nil {
		return nil
	}

	stack := debug.Stack()

	log.Printf("A snappy error occurred: %v\n", err)

	log.Printf("Stack trace:\n")

	for _, line := range strings.Split(string(stack), "\n") {
		log.Printf("%s\n", line)
	}

	return err
}
