// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap"
)

// AppActivator is a thing that activates the app that is a service in the
// system.
type AppActivator struct {
	Name string
	// Type describes the type of the unit, either timer or socket
	Type    string
	Active  bool
	Enabled bool
}

// AppInfo describes a single snap application.
type AppInfo struct {
	Snap        string           `json:"snap,omitempty"`
	Name        string           `json:"name"`
	DesktopFile string           `json:"desktop-file,omitempty"`
	Daemon      string           `json:"daemon,omitempty"`
	DaemonScope snap.DaemonScope `json:"daemon-scope,omitempty"`
	Enabled     bool             `json:"enabled,omitempty"`
	Active      bool             `json:"active,omitempty"`
	CommonID    string           `json:"common-id,omitempty"`
	Activators  []AppActivator   `json:"activators,omitempty"`
}

// IsService returns true if the application is a background daemon.
func (a *AppInfo) IsService() bool {
	if a == nil {
		return false
	}
	if a.Daemon == "" {
		return false
	}

	return true
}

// AppOptions represent the options of the Apps call.
type AppOptions struct {
	// If Service is true, only return apps that are services
	// (app.IsService() is true); otherwise, return all.
	Service bool
	// Global if set, returns only the global status of the services. This
	// is only relevant for user services, where we either return the status
	// of the services for the current user, or the global enable status.
	// For root-users, global is always implied.
	Global bool
}

// Apps returns information about all matching apps. Each name can be
// either a snap or a snap.app. If names is empty, list all (that
// satisfy opts).
func (client *Client) Apps(names []string, opts AppOptions) ([]*AppInfo, error) {
	q := make(url.Values)
	if len(names) > 0 {
		q.Add("names", strings.Join(names, ","))
	}
	if opts.Service {
		q.Add("select", "service")
	}
	if opts.Global {
		q.Add("global", fmt.Sprintf("%t", opts.Global))
	}

	var appInfos []*AppInfo
	_ := mylog.Check2(client.doSync("GET", "/v2/apps", q, nil, nil, &appInfos))

	return appInfos, err
}

// LogOptions represent the options of the Logs call.
type LogOptions struct {
	N      int  // The maximum number of log lines to retrieve initially. If <0, no limit.
	Follow bool // Whether to continue returning new lines as they appear
}

// A Log holds the information of a single syslog entry
type Log struct {
	Timestamp time.Time `json:"timestamp"` // Timestamp of the event, in RFC3339 format to µs precision.
	Message   string    `json:"message"`   // The log message itself
	SID       string    `json:"sid"`       // The syslog identifier
	PID       string    `json:"pid"`       // The process identifier
}

// String will format the log entry with the timestamp in the local timezone
func (l Log) String() string {
	return l.fmtLog(time.Local)
}

// StringInUTC will format the log entry with the timestamp in UTC
func (l Log) StringInUTC() string {
	return l.fmtLog(time.UTC)
}

func (l Log) fmtLog(timezone *time.Location) string {
	if timezone == nil {
		timezone = time.Local
	}

	return fmt.Sprintf("%s %s[%s]: %s", l.Timestamp.In(timezone).Format(time.RFC3339), l.SID, l.PID, l.Message)
}

// Logs asks for the logs of a series of services, by name.
func (client *Client) Logs(names []string, opts LogOptions) (<-chan Log, error) {
	query := url.Values{}
	if len(names) > 0 {
		query.Set("names", strings.Join(names, ","))
	}
	query.Set("n", strconv.Itoa(opts.N))
	if opts.Follow {
		query.Set("follow", strconv.FormatBool(opts.Follow))
	}

	rsp := mylog.Check2(client.raw(context.Background(), "GET", "/v2/logs", query, nil, nil))

	if rsp.StatusCode != 200 {
		var r response
		defer rsp.Body.Close()
		mylog.Check(decodeInto(rsp.Body, &r))

		return nil, r.err(client, rsp.StatusCode)
	}

	ch := make(chan Log, 20)
	go func() {
		// logs come in application/json-seq, described in RFC7464: it's
		// a series of <RS><arbitrary, valid JSON><LF>. Decoders are
		// expected to skip invalid or truncated or empty records.
		scanner := bufio.NewScanner(rsp.Body)
		for scanner.Scan() {
			buf := scanner.Bytes() // the scanner prunes the ending LF
			if len(buf) < 1 {
				// truncated record? skip
				continue
			}
			idx := bytes.IndexByte(buf, 0x1E) // find the initial RS
			if idx < 0 {
				// no RS? skip
				continue
			}
			buf = buf[idx+1:] // drop the initial RS
			var log Log
			mylog.Check(json.Unmarshal(buf, &log))
			// truncated/corrupted/binary record? skip

			ch <- log
		}
		close(ch)
		rsp.Body.Close()
	}()

	return ch, nil
}

type UserSelection int

const (
	UserSelectionList UserSelection = iota
	UserSelectionSelf
	UserSelectionAll
)

// UserSelector is a support structure for correctly translating a way of
// representing both a list of user-names, and specific keywords like "self"
// and "all" for JSON marshalling.
//
// When "Selector == UserSelectionList" then Names is used as the data source and
// the data is treated like a list of strings.
// When "Selector == UserSelectionSelf|UserSelectionAll", then the data source will
// be a single string that represent this in the form of "self|all".
type UserSelector struct {
	Names    []string
	Selector UserSelection
}

// UserList returns a decoded list of users which takes any keyword into account.
// Takes the current user to be able to handle special keywords like 'user'.
func (us *UserSelector) UserList(currentUser *user.User) ([]string, error) {
	switch us.Selector {
	case UserSelectionList:
		return us.Names, nil
	case UserSelectionSelf:
		if currentUser == nil {
			return nil, fmt.Errorf(`internal error: for "self" the current user must be provided`)
		}
		if currentUser.Uid == "0" {
			return nil, fmt.Errorf(`cannot use "self" for root user`)
		}
		return []string{currentUser.Username}, nil
	case UserSelectionAll:
		// Empty list indicates all.
		return nil, nil
	}
	return nil, fmt.Errorf("internal error: unsupported selector %d specified", us.Selector)
}

func (us UserSelector) MarshalJSON() ([]byte, error) {
	switch us.Selector {
	case UserSelectionList:
		return json.Marshal(us.Names)
	case UserSelectionSelf:
		return json.Marshal("self")
	case UserSelectionAll:
		return json.Marshal("all")
	default:
		return nil, fmt.Errorf("internal error: unsupported selector %d specified", us.Selector)
	}
}

func (us *UserSelector) UnmarshalJSON(b []byte) error {
	// Try treating it as a list of usernames first
	var users []string
	if mylog.Check(json.Unmarshal(b, &users)); err == nil {
		us.Names = users
		us.Selector = UserSelectionList
		return nil
	}

	// Fallback to string, which would indicate a keyword
	var s string
	mylog.Check(json.Unmarshal(b, &s))

	switch s {
	case "self":
		us.Selector = UserSelectionSelf
	case "all":
		us.Selector = UserSelectionAll
	default:
		return fmt.Errorf(`cannot unmarshal, expected one of: "self", "all"`)
	}
	return nil
}

type ScopeSelector []string

func (ss *ScopeSelector) UnmarshalJSON(b []byte) error {
	var scopes []string
	mylog.Check(json.Unmarshal(b, &scopes))

	if len(scopes) > 2 {
		return fmt.Errorf("unexpected number of scopes: %v", scopes)
	}

	for _, s := range scopes {
		switch s {
		case "system", "user":
		default:
			return fmt.Errorf(`cannot unmarshal, expected one of: "system", "user"`)
		}
	}
	*ss = scopes
	return nil
}

// ErrNoNames is returned by Start, Stop, or Restart, when the given
// list of things on which to operate is empty.
var ErrNoNames = errors.New(`"names" must not be empty`)

type appInstruction struct {
	Action string        `json:"action"`
	Names  []string      `json:"names"`
	Scope  ScopeSelector `json:"scope,omitempty"`
	Users  UserSelector  `json:"users,omitempty"`
	StartOptions
	StopOptions
	RestartOptions
}

// StartOptions represent the different options of the Start call.
type StartOptions struct {
	// Enable, as well as starting, the listed services. A
	// disabled service does not start on boot.
	Enable bool `json:"enable,omitempty"`
}

// Start services.
//
// It takes a list of names that can be snaps, of which all their
// services are started, or snap.service which are individual
// services to start; it shouldn't be empty.
func (client *Client) Start(names []string, scope ScopeSelector, users UserSelector, opts StartOptions) (changeID string, err error) {
	if len(names) == 0 {
		return "", ErrNoNames
	}

	buf := mylog.Check2(json.Marshal(appInstruction{
		Action:       "start",
		Names:        names,
		Scope:        scope,
		Users:        users,
		StartOptions: opts,
	}))

	return client.doAsync("POST", "/v2/apps", nil, nil, bytes.NewReader(buf))
}

// StopOptions represent the different options of the Stop call.
type StopOptions struct {
	// Disable, as well as stopping, the listed services. A
	// service that is not disabled starts on boot.
	Disable bool `json:"disable,omitempty"`
}

// Stop services.
//
// It takes a list of names that can be snaps, of which all their
// services are stopped, or snap.service which are individual
// services to stop; it shouldn't be empty.
func (client *Client) Stop(names []string, scope ScopeSelector, users UserSelector, opts StopOptions) (changeID string, err error) {
	if len(names) == 0 {
		return "", ErrNoNames
	}

	buf := mylog.Check2(json.Marshal(appInstruction{
		Action:      "stop",
		Names:       names,
		Scope:       scope,
		Users:       users,
		StopOptions: opts,
	}))

	return client.doAsync("POST", "/v2/apps", nil, nil, bytes.NewReader(buf))
}

// RestartOptions represent the different options of the Restart call.
type RestartOptions struct {
	// Reload the services, if possible (i.e. if the App has a
	// ReloadCommand, invoque it), instead of restarting.
	Reload bool `json:"reload,omitempty"`
}

// Restart services.
//
// It takes a list of names that can be snaps, of which all their
// services are restarted, or snap.service which are individual
// services to restart; it shouldn't be empty. If the service is not
// running, starts it.
func (client *Client) Restart(names []string, scope ScopeSelector, users UserSelector, opts RestartOptions) (changeID string, err error) {
	if len(names) == 0 {
		return "", ErrNoNames
	}

	buf := mylog.Check2(json.Marshal(appInstruction{
		Action:         "restart",
		Names:          names,
		Scope:          scope,
		Users:          users,
		RestartOptions: opts,
	}))

	return client.doAsync("POST", "/v2/apps", nil, nil, bytes.NewReader(buf))
}
