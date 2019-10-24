// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
)

var restApi = []*Command{
	rootCmd,
	sessionInfoCmd,
	servicesCmd,
}

var (
	rootCmd = &Command{
		Path: "/",
		GET:  nil,
	}

	sessionInfoCmd = &Command{
		Path: "/v1/session-info",
		GET:  sessionInfo,
	}

	servicesCmd = &Command{
		Path: "/v1/services",
		POST: postServices,
	}
)

func sessionInfo(c *Command, r *http.Request) Response {
	m := map[string]interface{}{
		"version": c.s.Version,
	}
	return SyncResponse(m)
}

type serviceInstruction struct {
	Action   string   `json:"action"`
	Services []string `json:"services"`
}

var (
	stopTimeout = time.Duration(timeout.DefaultTimeout)
	killWait    = 5 * time.Second
)

func serviceStart(inst *serviceInstruction, sysd systemd.Systemd) Response {
	// Refuse to start non-snap services
	for _, service := range inst.Services {
		if !strings.HasPrefix(service, "snap.") {
			return InternalError("cannot start non-snap service %v", service)
		}
	}

	startErrors := make(map[string]string)
	var started []string
	for _, service := range inst.Services {
		if err := sysd.Start(service); err != nil {
			startErrors[service] = err.Error()
			break
		}
		started = append(started, service)
	}
	// If we got any failures, attempt to stop the services we started.
	if len(startErrors) != 0 {
		for _, service := range started {
			if err := sysd.Stop(service, stopTimeout); err != nil {
				startErrors[service] = err.Error()
			}
		}
	}
	if len(startErrors) == 0 {
		return SyncResponse(nil)
	}
	return SyncResponse(&resp{
		Type:   ResponseTypeError,
		Status: 500,
		Result: &errorResult{
			Message: "some services failed to start",
			Kind:    errorKindServiceControl,
			Value:   startErrors,
		},
	})
}

func serviceStop(inst *serviceInstruction, sysd systemd.Systemd) Response {
	// Refuse to start non-snap services
	for _, service := range inst.Services {
		if !strings.HasPrefix(service, "snap.") {
			return InternalError("cannot stop non-snap service %v", service)
		}
	}

	stopErrors := make(map[string]string)
	for _, service := range inst.Services {
		if err := sysd.Stop(service, stopTimeout); err != nil {
			stopErrors[service] = err.Error()
		}
	}
	if len(stopErrors) == 0 {
		return SyncResponse(nil)
	}
	return SyncResponse(&resp{
		Type:   ResponseTypeError,
		Status: 500,
		Result: &errorResult{
			Message: "some services failed to stop",
			Kind:    errorKindServiceControl,
			Value:   stopErrors,
		},
	})
}

func serviceDaemonReload(inst *serviceInstruction, sysd systemd.Systemd) Response {
	if len(inst.Services) != 0 {
		return InternalError("daemon-reload should not be called with any services")
	}
	if err := sysd.DaemonReload(); err != nil {
		return InternalError("cannot reload daemon: %v", err)
	}
	return SyncResponse(nil)
}

var serviceInstructionDispTable = map[string]func(*serviceInstruction, systemd.Systemd) Response{
	"start":         serviceStart,
	"stop":          serviceStop,
	"daemon-reload": serviceDaemonReload,
}

var systemdLock sync.Mutex

type dummyReporter struct{}

func (dummyReporter) Notify(string) {}

func postServices(c *Command, r *http.Request) Response {
	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		return BadRequest("unknown content type: %s", contentType)
	}

	decoder := json.NewDecoder(r.Body)
	var inst serviceInstruction
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("cannot decode request body into service instruction: %v", err)
	}
	impl := serviceInstructionDispTable[inst.Action]
	if impl == nil {
		return BadRequest("unknown action %s", inst.Action)
	}
	// Prevent multiple systemd actions from being carried out simultaneously
	systemdLock.Lock()
	defer systemdLock.Unlock()
	sysd := systemd.New(dirs.GlobalRootDir, systemd.UserMode, dummyReporter{})
	return impl(&inst, sysd)
}
