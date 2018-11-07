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
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
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
	Snap        string         `json:"snap,omitempty"`
	Name        string         `json:"name"`
	DesktopFile string         `json:"desktop-file,omitempty"`
	Daemon      string         `json:"daemon,omitempty"`
	Enabled     bool           `json:"enabled,omitempty"`
	Active      bool           `json:"active,omitempty"`
	CommonID    string         `json:"common-id,omitempty"`
	Activators  []AppActivator `json:"activators,omitempty"`
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

	var appInfos []*AppInfo
	_, err := client.doSync("GET", "/v2/apps", q, nil, nil, &appInfos)

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

func (l Log) String() string {
	return fmt.Sprintf("%s %s[%s]: %s", l.Timestamp.Format(time.RFC3339), l.SID, l.PID, l.Message)
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

	rsp, err := client.raw("GET", "/v2/logs", query, nil, nil)
	if err != nil {
		return nil, err
	}

	if rsp.StatusCode != 200 {
		var r response
		defer rsp.Body.Close()
		if err := decodeInto(rsp.Body, &r); err != nil {
			return nil, err
		}
		return nil, r.err(client)
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
			if err := json.Unmarshal(buf, &log); err != nil {
				// truncated/corrupted/binary record? skip
				continue
			}
			ch <- log
		}
		close(ch)
		rsp.Body.Close()
	}()

	return ch, nil
}

// ErrNoNames is returned by Start, Stop, or Restart, when the given
// list of things on which to operate is empty.
var ErrNoNames = errors.New(`"names" must not be empty`)

type appInstruction struct {
	Action string   `json:"action"`
	Names  []string `json:"names"`
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
func (client *Client) Start(names []string, opts StartOptions) (changeID string, err error) {
	if len(names) == 0 {
		return "", ErrNoNames
	}

	buf, err := json.Marshal(appInstruction{
		Action:       "start",
		Names:        names,
		StartOptions: opts,
	})
	if err != nil {
		return "", err
	}
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
func (client *Client) Stop(names []string, opts StopOptions) (changeID string, err error) {
	if len(names) == 0 {
		return "", ErrNoNames
	}

	buf, err := json.Marshal(appInstruction{
		Action:      "stop",
		Names:       names,
		StopOptions: opts,
	})
	if err != nil {
		return "", err
	}
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
func (client *Client) Restart(names []string, opts RestartOptions) (changeID string, err error) {
	if len(names) == 0 {
		return "", ErrNoNames
	}

	buf, err := json.Marshal(appInstruction{
		Action:         "restart",
		Names:          names,
		RestartOptions: opts,
	})
	if err != nil {
		return "", err
	}
	return client.doAsync("POST", "/v2/apps", nil, nil, bytes.NewReader(buf))
}

func (app *AppInfo) Notes() string {
	if !app.IsService() {
		return "-"
	}

	var notes = make([]string, 0, 2)
	var seenTimer, seenSocket bool
	for _, act := range app.Activators {
		switch act.Type {
		case "timer":
			seenTimer = true
		case "socket":
			seenSocket = true
		}
	}
	if seenTimer {
		notes = append(notes, "timer-activated")
	}
	if seenSocket {
		notes = append(notes, "socket-activated")
	}
	if len(notes) == 0 {
		return "-"
	}
	return strings.Join(notes, ",")
}

func AppInfosFromSnapAppInfos(apps []*snap.AppInfo) []AppInfo {
	// TODO: pass in an actual notifier here instead of null
	//       (Status doesn't _need_ it, but benefits from it)
	sysd := systemd.New(dirs.GlobalRootDir, progress.Null)

	out := make([]AppInfo, 0, len(apps))
	for _, app := range apps {
		appInfo := AppInfo{
			Snap:     app.Snap.InstanceName(),
			Name:     app.Name,
			CommonID: app.CommonID,
		}
		if fn := app.DesktopFile(); osutil.FileExists(fn) {
			appInfo.DesktopFile = fn
		}

		appInfo.Daemon = app.Daemon
		if !app.IsService() || !app.Snap.IsActive() {
			out = append(out, appInfo)
			continue
		}

		// collect all services for a single call to systemctl
		serviceNames := make([]string, 0, 1+len(app.Sockets)+1)
		serviceNames = append(serviceNames, app.ServiceName())

		sockSvcFileToName := make(map[string]string, len(app.Sockets))
		for _, sock := range app.Sockets {
			sockUnit := filepath.Base(sock.File())
			sockSvcFileToName[sockUnit] = sock.Name
			serviceNames = append(serviceNames, sockUnit)
		}
		if app.Timer != nil {
			timerUnit := filepath.Base(app.Timer.File())
			serviceNames = append(serviceNames, timerUnit)
		}

		// sysd.Status() makes sure that we get only the units we asked
		// for and raises an error otherwise
		sts, err := sysd.Status(serviceNames...)
		if err != nil {
			logger.Noticef("cannot get status of services of app %q: %v", app.Name, err)
			continue
		}
		if len(sts) != len(serviceNames) {
			logger.Noticef("cannot get status of services of app %q: expected %v results, got %v", app.Name, len(serviceNames), len(sts))
			continue
		}
		for _, st := range sts {
			switch filepath.Ext(st.UnitName) {
			case ".service":
				appInfo.Enabled = st.Enabled
				appInfo.Active = st.Active
			case ".timer":
				appInfo.Activators = append(appInfo.Activators, AppActivator{
					Name:    app.Name,
					Enabled: st.Enabled,
					Active:  st.Active,
					Type:    "timer",
				})
			case ".socket":
				appInfo.Activators = append(appInfo.Activators, AppActivator{
					Name:    sockSvcFileToName[st.UnitName],
					Enabled: st.Enabled,
					Active:  st.Active,
					Type:    "socket",
				})
			}
		}
		out = append(out, appInfo)
	}

	return out
}
