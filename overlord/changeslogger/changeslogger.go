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
	seenChanges   map[string]ChangeInfo
	changeLogPath string
	retryCount    map[string]int
}

// New creates a new ChangesLogger manager
func New(s *state.State) *Manager {
	return &Manager{
		state:         s,
		seenChanges:   make(map[string]ChangeInfo),
		changeLogPath: dirs.SnapdChangesLog,
		retryCount:    make(map[string]int),
	}
}

// Ensure checks for any state changes and logs them
func (m *Manager) Ensure() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.Lock()
	changes := m.state.Changes()

	// Extract info for all changes while holding lock
	changeInfos := make(map[string]ChangeInfo)
	for _, chg := range changes {
		changeInfos[chg.ID()] = ExtractChangeInfo(chg)
	}
	m.state.Unlock()

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
			delete(m.retryCount, id)
		}
	}

	return nil
}

// logChange writes a change entry to the log file in APT history.log format
func (m *Manager) logChange(info ChangeInfo) error {
	// Ensure the directory exists
	logDir := filepath.Dir(m.changeLogPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("cannot create log directory: %v", err)
	}

	// Open the log file for appending
	f, err := os.OpenFile(m.changeLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open log file: %v", err)
	}
	defer f.Close()

	// Write the change entry as Key: Value pairs
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

	retryCount := m.retryCount[info.ID]
	if retryCount > 0 {
		lines = append(lines, fmt.Sprintf("RetryCount: %d", retryCount))
	}

	if info.Error != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", info.Error))
	}

	// Write all lines followed by a blank line separator
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			return fmt.Errorf("cannot write log entry: %v", err)
		}
	}

	// Write blank line separator between entries
	if _, err := f.WriteString("\n"); err != nil {
		return fmt.Errorf("cannot write log separator: %v", err)
	}

	return nil
}

// StartUp performs any necessary initialization
func (m *Manager) StartUp() error {
	// Ensure the log directory exists on startup
	logDir := filepath.Dir(m.changeLogPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("cannot create changes log directory: %v", err)
	}

	if stat, err := os.Stat(m.changeLogPath); err == nil {
		logger.Debugf("Changes log file already exists at %q (size: %d)", m.changeLogPath, stat.Size())
	}

	return nil
}
