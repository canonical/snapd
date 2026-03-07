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

package changeslogger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

// ChangeInfo stores immutable information about a change for logging
type ChangeInfo struct {
	ID        string
	Kind      string
	Summary   string
	Status    string
	SpawnTime time.Time
	ReadyTime time.Time
	Error     string
}

// ExtractChangeInfo extracts necessary information from a change (must be called with state locked)
func ExtractChangeInfo(chg *state.Change) ChangeInfo {
	info := ChangeInfo{
		ID:        chg.ID(),
		Kind:      chg.Kind(),
		Summary:   chg.Summary(),
		Status:    chg.Status().String(),
		SpawnTime: chg.SpawnTime(),
		ReadyTime: chg.ReadyTime(),
	}
	if chg.Err() != nil {
		info.Error = chg.Err().Error()
	}
	return info
}

// Manager logs changes to a file for audit/monitoring purposes
type Manager struct {
	state         *state.State
	mu            sync.Mutex
	seenChanges   map[string]ChangeInfo // indexed by change ID
	changeLogPath string
}

// New creates a new ChangesLogger manager
func New(s *state.State) *Manager {
	return &Manager{
		state:         s,
		seenChanges:   make(map[string]ChangeInfo),
		changeLogPath: dirs.SnapdChangesLog,
	}
}

// Ensure checks for any state changes and logs them
func (m *Manager) Ensure() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	changeInfos, err := m.readState()
	if err != nil {
		return err
	}

	if changeInfos == nil {
		return nil
	}

	// Track which change IDs we've seen this pass
	currentChangeIDs := make(map[string]bool)

	for id, info := range changeInfos {
		currentChangeIDs[id] = true

		// Check if this is a new change or if it has changed status
		if oldInfo, seen := m.seenChanges[id]; !seen || oldInfo.Status != info.Status {
			// Log the change
			if err := m.logChange(info); err != nil {
				// Log the error but don't fail the ensure
				logger.Noticef("Failed to log change %s: %v", id, err)
			}
			// Store the info to compare next time
			m.seenChanges[id] = info
		}
	}

	// Clean up old tracked changes that are no longer in state
	for id := range m.seenChanges {
		if !currentChangeIDs[id] {
			delete(m.seenChanges, id)
		}
	}

	return nil
}

// readState reads the enabled config and extracts change info from state.
// It returns a map of change ID to ChangeInfo, or a nil map if logging
// is disabled.
func (m *Manager) readState() (changeInfos map[string]ChangeInfo, err error) {
	m.state.Lock()
	defer m.state.Unlock()

	// Check if changes-log is enabled via core config (default: enabled)
	tr := config.NewTransaction(m.state)
	var enabled bool
	err = tr.Get("core", "system.enable-changes-log", &enabled)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}

	if err == nil && !enabled {
		// Explicitly set to false: skip logging
		return nil, nil
	}

	changes := m.state.Changes()
	changeInfos = make(map[string]ChangeInfo, len(changes))
	for _, chg := range changes {
		changeInfos[chg.ID()] = ExtractChangeInfo(chg)
	}

	return changeInfos, nil
}

// logChange writes a change entry to the log file in APT history.log format
func (m *Manager) logChange(info ChangeInfo) error {
	// Ensure the directory exists rather than relying entirely in packaging to create it
	logDir := filepath.Dir(m.changeLogPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("cannot create log directory: %v", err)
	}

	f, err := os.OpenFile(m.changeLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open log file: %v", err)
	}
	defer f.Close()

	// Each record is `Key: Value` pairs, one per line, followed by the YAML record separator `---` on a line by itself. This format is simple to write and parse, and is valid YAML (with the separator) if we want to use YAML tools in the future.
	// This makes the log file human-readable, simple to parse with basic dev tools, and also valid YAML for more advanced parsing if needed.
	lines := []string{
		fmt.Sprintf("Timestamp: %s", time.Now().Format(time.RFC3339)),
		fmt.Sprintf("ID: %s", info.ID),
		fmt.Sprintf("Kind: %s", info.Kind),
		fmt.Sprintf("Summary: %s", info.Summary),
		fmt.Sprintf("Status: %s", info.Status),
		fmt.Sprintf("SpawnTime: %s", info.SpawnTime.Format(time.RFC3339)),
	}

	if !info.ReadyTime.IsZero() {
		lines = append(lines, fmt.Sprintf("ReadyTime: %s", info.ReadyTime.Format(time.RFC3339)))
	}

	if info.Error != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", info.Error))
	}

	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			return fmt.Errorf("cannot write log entry: %v", err)
		}
	}

	if _, err := f.WriteString("---\n"); err != nil {
		return fmt.Errorf("cannot write log separator: %v", err)
	}

	return nil
}

// StartUp performs any necessary initialization
func (m *Manager) StartUp() error {
	// Ensure the log directory exists on startup
	// This is still created in Ensure/logChange to be resilient against the directory being deleted while snapd is running
	logDir := filepath.Dir(m.changeLogPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("cannot create changes log directory: %v", err)
	}

	if stat, err := os.Stat(m.changeLogPath); err == nil {
		logger.Debugf("Changes log file already exists at %q (size: %d)", m.changeLogPath, stat.Size())
	}

	return nil
}
