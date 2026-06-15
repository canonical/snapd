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

package snapstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

const toolGetServiceLogs = "snap_get_service_logs"

type getServiceLogsTool struct{}

var readServiceLogsFromApp = readServiceLogs

type getServiceLogsArgs struct {
	ServiceName string     `json:"service_name" mcp:"description=Service name in the form <snap>.<app>."`
	Lines       int        `json:"lines,omitempty" mcp:"description=Maximum number of log lines to fetch (default: 100, use -1 for all)."`
	Since       *time.Time `json:"since,omitempty" mcp:"description=Optional inclusive lower time bound in RFC3339 format."`
	Until       *time.Time `json:"until,omitempty" mcp:"description=Optional inclusive upper time bound in RFC3339 format."`
	StderrOnly  bool       `json:"stderr_only,omitempty" mcp:"description=If true, only include high-priority journal entries (priority 0..3)."`
}

type getServiceLogsItem struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	SID       string    `json:"sid"`
	PID       string    `json:"pid"`
	Priority  int       `json:"priority,omitempty"`
}

type getServiceLogsResult struct {
	ServiceName string               `json:"service_name"`
	Logs        []getServiceLogsItem `json:"logs"`
}

var getServiceLogsToolDescriptor = mcp.ToolDescriptor{
	Name:         toolGetServiceLogs,
	Title:        "Get snap service logs",
	Description:  "Get logs for one snap service with optional journalctl time-window and stderr-only filtering (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(getServiceLogsArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(getServiceLogsResult{}),
}

func (getServiceLogsTool) Descriptor() mcp.ToolDescriptor {
	return getServiceLogsToolDescriptor
}

func (getServiceLogsTool) ArgsType() any {
	return &getServiceLogsArgs{}
}

func (getServiceLogsTool) ValidateArgs(args any) error {
	v, ok := args.(*getServiceLogsArgs)
	if !ok {
		return fmt.Errorf("invalid typed args for get service logs tool")
	}
	if strings.TrimSpace(v.ServiceName) == "" {
		return fmt.Errorf("service_name must not be empty")
	}
	if !strings.Contains(v.ServiceName, ".") {
		return fmt.Errorf("service_name must be in the form <snap>.<app>")
	}
	if v.Lines < -1 {
		return fmt.Errorf("lines must be greater than zero, or -1")
	}
	if v.Since != nil && v.Until != nil && v.Since.After(*v.Until) {
		return fmt.Errorf("since must not be after until")
	}
	return nil
}

func (getServiceLogsTool) ResultType() any {
	return &getServiceLogsResult{}
}

func (getServiceLogsTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	filterArgs, ok := args.(*getServiceLogsArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for get service logs tool")
	}
	serviceName := strings.TrimSpace(filterArgs.ServiceName)
	if serviceName == "" {
		return nil, errors.New("service_name must not be empty")
	}
	if !strings.Contains(serviceName, ".") {
		return nil, errors.New("service_name must be in the form <snap>.<app>")
	}

	lines := 100
	if filterArgs.Lines != 0 {
		if filterArgs.Lines < -1 {
			return nil, errors.New("lines must be greater than zero, or -1")
		}
		lines = filterArgs.Lines
	}

	sinceValue := ""
	var sinceTime time.Time
	if filterArgs.Since != nil {
		sinceValue = filterArgs.Since.Format(time.RFC3339)
		sinceTime = *filterArgs.Since
	}

	untilValue := ""
	var untilTime time.Time
	if filterArgs.Until != nil {
		untilValue = filterArgs.Until.Format(time.RFC3339)
		untilTime = *filterArgs.Until
	}

	if !sinceTime.IsZero() && !untilTime.IsZero() && sinceTime.After(untilTime) {
		return nil, errors.New("since must not be after until")
	}

	stderrOnly := filterArgs.StderrOnly

	serviceApp, err := serviceFromState(st, serviceName)
	if err != nil {
		return nil, err
	}

	entries, err := readServiceLogsFromApp(serviceApp, lines, sinceValue, untilValue, stderrOnly)
	if err != nil {
		return nil, fmt.Errorf("cannot get logs for %q: %w", serviceName, err)
	}

	logs := make([]getServiceLogsItem, 0, len(entries))
	for _, entry := range entries {
		item := getServiceLogsItem{}
		if v, ok := entry["timestamp"].(time.Time); ok {
			item.Timestamp = v
		} else if v, ok := entry["timestamp"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				item.Timestamp = parsed
			}
		}
		if v, ok := entry["message"].(string); ok {
			item.Message = v
		}
		if v, ok := entry["sid"].(string); ok {
			item.SID = v
		}
		if v, ok := entry["pid"].(string); ok {
			item.PID = v
		}
		if v, ok := entry["priority"].(float64); ok {
			item.Priority = int(v)
		} else if v, ok := entry["priority"].(int); ok {
			item.Priority = v
		}
		logs = append(logs, item)
	}

	return getServiceLogsResult{ServiceName: serviceName, Logs: logs}, nil
}

func (getServiceLogsTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[getServiceLogsArgs](args)
	return err
}

func (getServiceLogsTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[getServiceLogsArgs](args)
	if err != nil {
		return nil, err
	}
	return getServiceLogsTool{}.CallWithArgs(ctx, st, parsedArgs)
}

func serviceFromState(st *state.State, serviceName string) (*snap.AppInfo, error) {
	snapName, appName, found := strings.Cut(serviceName, ".")
	if !found || snapName == "" || appName == "" {
		return nil, fmt.Errorf("service_name must be in the form <snap>.<app>")
	}

	st.Lock()
	defer st.Unlock()

	var snapst SnapState
	if err := Get(st, snapName, &snapst); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, fmt.Errorf("cannot find service %q", serviceName)
		}
		return nil, fmt.Errorf("cannot consult state for %q: %w", serviceName, err)
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		if err == ErrNoCurrent {
			return nil, fmt.Errorf("cannot find service %q", serviceName)
		}
		return nil, fmt.Errorf("cannot read snap details for %q: %w", snapName, err)
	}

	app := info.Apps[appName]
	if app == nil || !app.IsService() {
		return nil, fmt.Errorf("cannot find service %q", serviceName)
	}

	return app, nil
}

func readServiceLogs(serviceApp *snap.AppInfo, lines int, since, until string, stderrOnly bool) ([]map[string]any, error) {
	if serviceApp == nil {
		return nil, errors.New("service app must not be nil")
	}

	args := []string{"-o", "json", "--no-pager"}
	if lines < 0 {
		args = append(args, "--no-tail")
	} else {
		args = append(args, "-n", strconv.Itoa(lines))
	}
	if since != "" {
		args = append(args, "--since", since)
	}
	if until != "" {
		args = append(args, "--until", until)
	}
	if stderrOnly {
		args = append(args, "-p", "0..3")
	}

	includeNamespaces := false
	if err := systemd.EnsureAtLeast(245); err == nil {
		includeNamespaces = true
	} else if !systemd.IsSystemdTooOld(err) {
		return nil, fmt.Errorf("cannot get systemd version: %v", err)
	}
	if includeNamespaces {
		args = append(args, "--namespace=*")
	}

	args = append(args, "-u", serviceApp.ServiceName())

	rc, err := osutil.StreamCommand("journalctl", args...)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	entries, err := decodeJournalEntries(rc)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func decodeJournalEntries(reader io.Reader) ([]map[string]any, error) {
	decoder := json.NewDecoder(reader)
	entries := make([]map[string]any, 0)
	for {
		var entry systemd.Log
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		timestamp, err := entry.Time()
		if err != nil {
			timestamp = time.Time{}
		}

		mapped := map[string]any{
			"timestamp": timestamp.UTC(),
			"message":   entry.Message(),
			"sid":       entry.SID(),
			"pid":       entry.PID(),
		}
		if priority, ok := journalPriority(entry); ok {
			mapped["priority"] = priority
		}

		entries = append(entries, mapped)
	}

	return entries, nil
}

func journalPriority(entry systemd.Log) (int, bool) {
	rawPriority, ok := entry["PRIORITY"]
	if !ok || rawPriority == nil {
		return 0, false
	}

	var single string
	if err := json.Unmarshal(*rawPriority, &single); err == nil {
		priority, err := strconv.Atoi(single)
		if err != nil {
			return 0, false
		}
		return priority, true
	}

	var list []string
	if err := json.Unmarshal(*rawPriority, &list); err != nil || len(list) == 0 {
		return 0, false
	}
	priority, err := strconv.Atoi(list[0])
	if err != nil {
		return 0, false
	}
	return priority, true
}
