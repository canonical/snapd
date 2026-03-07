// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package changeslogger_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/changeslogger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type changesLoggerSuite struct {
	st *state.State
	testutil.BaseTest
}

var _ = Suite(&changesLoggerSuite{})

func (s *changesLoggerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.st = state.New(nil)
}

func (s *changesLoggerSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *changesLoggerSuite) setChangesLogEnabled(c *C, enabled bool) {
	s.st.Lock()
	defer s.st.Unlock()
	tr := config.NewTransaction(s.st)
	err := tr.Set("core", "system.enable-changes-log", enabled)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *changesLoggerSuite) TestNew(c *C) {
	m := changeslogger.New(s.st)
	c.Assert(m, NotNil)
}

func (s *changesLoggerSuite) TestEnsureLogsWhenConfigUnset(c *C) {
	logDir, err := os.MkdirTemp("", "snapd-changes-log-test-")
	c.Assert(err, IsNil)
	defer os.RemoveAll(logDir)

	logFile := filepath.Join(logDir, "changes.log")

	m := changeslogger.NewTestManager(s.st, logFile)

	// Create a change in state without setting the config
	s.st.Lock()
	s.st.NewChange("install", "Install snap")
	s.st.Unlock()

	// Ensure should log since default is enabled
	err = m.Ensure()
	c.Assert(err, IsNil)

	// Log file should exist
	_, err = os.Stat(logFile)
	c.Assert(err, IsNil)
}

func (s *changesLoggerSuite) TestEnsureSkipsWhenExplicitlyDisabled(c *C) {
	logDir, err := os.MkdirTemp("", "snapd-changes-log-test-")
	c.Assert(err, IsNil)
	defer os.RemoveAll(logDir)

	logFile := filepath.Join(logDir, "changes.log")

	m := changeslogger.NewTestManager(s.st, logFile)

	// Explicitly disable the config
	s.setChangesLogEnabled(c, false)

	// Create a change in state
	s.st.Lock()
	s.st.NewChange("install", "Install snap")
	s.st.Unlock()

	// Ensure should not log anything
	err = m.Ensure()
	c.Assert(err, IsNil)

	// Log file should not exist
	_, err = os.Stat(logFile)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *changesLoggerSuite) TestEnsureLogsNewChange(c *C) {
	logDir, err := os.MkdirTemp("", "snapd-changes-log-test-")
	c.Assert(err, IsNil)
	defer os.RemoveAll(logDir)

	logFile := filepath.Join(logDir, "changes.log")

	m := changeslogger.NewTestManager(s.st, logFile)

	// Create a change in state
	s.st.Lock()
	chg := s.st.NewChange("install", "Install snap")
	changeID := chg.ID()
	s.st.Unlock()

	// Run ensure to log the change
	err = m.Ensure()
	c.Assert(err, IsNil)

	// Check the log file exists and contains the entry
	_, err = os.Stat(logFile)
	c.Assert(err, IsNil)

	// Read and verify the log entry
	f, err := os.Open(logFile)
	c.Assert(err, IsNil)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	entry := make(map[string]string)

	// Parse the first log entry (Key: Value format)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			// YAML record separator separates entries
			break
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			entry[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	c.Assert(entry["ID"], Equals, changeID)
	c.Assert(entry["Kind"], Equals, "install")
	c.Assert(entry["Summary"], Equals, "Install snap")
	c.Assert(entry["Status"], Equals, "Hold")
	c.Assert(entry["Timestamp"], Not(Equals), "")
}

func (s *changesLoggerSuite) TestEnsureLogsStatusChange(c *C) {
	logDir, err := os.MkdirTemp("", "snapd-changes-log-test-")
	c.Assert(err, IsNil)
	defer os.RemoveAll(logDir)

	logFile := filepath.Join(logDir, "changes.log")

	m := changeslogger.NewTestManager(s.st, logFile)

	// Create a change in state
	s.st.Lock()
	chg := s.st.NewChange("install", "Install snap")
	s.st.Unlock()

	// First ensure logs the creation
	err = m.Ensure()
	c.Assert(err, IsNil)

	// Change the status
	s.st.Lock()
	chg.SetStatus(state.DoneStatus)
	s.st.Unlock()

	// Second ensure should log the status change
	err = m.Ensure()
	c.Assert(err, IsNil)

	// Read and verify both log entries
	f, err := os.Open(logFile)
	c.Assert(err, IsNil)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var entries []map[string]string
	currentEntry := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			// YAML record separator separates entries
			if len(currentEntry) > 0 {
				entries = append(entries, currentEntry)
				currentEntry = make(map[string]string)
			}
		} else {
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) == 2 {
				currentEntry[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Don't forget the last entry if file doesn't end with YAML record separator
	if len(currentEntry) > 0 {
		entries = append(entries, currentEntry)
	}

	c.Assert(len(entries), Equals, 2)
	c.Assert(entries[0]["Status"], Equals, "Hold")
	c.Assert(entries[1]["Status"], Equals, "Done")
}

func (s *changesLoggerSuite) TestStartUpCreatesDirectory(c *C) {
	logDir, err := os.MkdirTemp("", "snapd-changes-log-test-")
	c.Assert(err, IsNil)
	defer os.RemoveAll(logDir)

	logFile := filepath.Join(logDir, "subdir", "changes.log")

	m := changeslogger.NewTestManager(s.st, logFile)

	err = m.StartUp()
	c.Assert(err, IsNil)

	// Check that the directory was created
	stat, err := os.Stat(filepath.Dir(logFile))
	c.Assert(err, IsNil)
	c.Assert(stat.IsDir(), Equals, true)
}
