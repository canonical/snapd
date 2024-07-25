// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/client"
)

var (
	timeout = testutil.HostScaledTimeout(80 * time.Millisecond)
)

func Test(t *testing.T) { TestingT(t) }

type clientSuite struct {
	testutil.BaseTest

	cli *client.Client

	server  *http.Server
	handler http.Handler
}

var _ = Suite(&clientSuite{})

func (s *clientSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.handler = nil

	s.server = &http.Server{Handler: s}
	for _, uid := range []int{1000, 42} {
		sock := fmt.Sprintf("%s/%d/snapd-session-agent.socket", dirs.XdgRuntimeDirBase, uid)
		err := os.MkdirAll(filepath.Dir(sock), 0755)
		c.Assert(err, IsNil)
		l, err := net.Listen("unix", sock)
		c.Assert(err, IsNil)
		go func(l net.Listener) {
			err := s.server.Serve(l)
			c.Check(err, Equals, http.ErrServerClosed)
		}(l)
	}

	s.cli = client.New()
}

func (s *clientSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")

	err := s.server.Shutdown(context.Background())
	c.Check(err, IsNil)
}

func (s *clientSuite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.handler == nil {
		w.WriteHeader(500)
		return
	}
	s.handler.ServeHTTP(w, r)
}

func (s *clientSuite) TestBadJsonResponse(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"type":`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Check(si, DeepEquals, map[int]client.SessionInfo{})
	c.Check(err, ErrorMatches, `cannot decode "{\\"type\\":": unexpected EOF`)
}

func (s *clientSuite) TestAgentTimeout(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay one of the agents from responding, but don't
		// stick around if the client disconnects.
		if r.Host == "1000" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Second):
				c.Fatal("Request context was not cancelled")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": {
    "version": "42"
  }
}`))
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	si, err := s.cli.SessionInfo(ctx)

	// An error is reported, and we receive information about the
	// agent that replied on time.
	c.Assert(err, ErrorMatches, `Get \"?http://1000/v1/session-info\"?: context deadline exceeded`)
	c.Check(si, DeepEquals, map[int]client.SessionInfo{
		42: {Version: "42"},
	})
}

func (s *clientSuite) TestSessionInfo(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": {
    "version": "42"
  }
}`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Assert(err, IsNil)
	c.Check(si, DeepEquals, map[int]client.SessionInfo{
		42:   {Version: "42"},
		1000: {Version: "42"},
	})
}

func (s *clientSuite) TestSessionInfoError(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "message": "something bad happened"
  }
}`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Check(si, DeepEquals, map[int]client.SessionInfo{})
	c.Check(err, ErrorMatches, "something bad happened")
	c.Check(err, DeepEquals, &client.Error{
		Kind:    "",
		Message: "something bad happened",
		Value:   nil,
	})
}

func (s *clientSuite) TestSessionInfoWrongResultType(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": ["a", "list"]
}`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Check(si, DeepEquals, map[int]client.SessionInfo{})
	c.Check(err, ErrorMatches, `json: cannot unmarshal array into Go value of type client.SessionInfo`)
}

func (s *clientSuite) TestServicesDaemonReload(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	err := s.cli.ServicesDaemonReload(context.Background())
	c.Assert(err, IsNil)
}

func (s *clientSuite) TestServicesDaemonReloadError(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "message": "something bad happened"
  }
}`))
	})
	err := s.cli.ServicesDaemonReload(context.Background())
	c.Check(err, ErrorMatches, "something bad happened")
	c.Check(err, DeepEquals, &client.Error{
		Kind:    "",
		Message: "something bad happened",
		Value:   nil,
	})
}

func (s *clientSuite) TestServicesStart(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	startFailures, stopFailures, err := s.cli.ServicesStart(context.Background(), []string{"service1.service", "service2.service"}, client.ClientServicesStartOptions{})
	c.Assert(err, IsNil)
	c.Check(startFailures, HasLen, 0)
	c.Check(stopFailures, HasLen, 0)
}

func (s *clientSuite) TestServicesStartWithDisabledServices(c *C) {
	var n int32
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		decoder := json.NewDecoder(r.Body)
		var inst client.ServiceInstruction
		c.Assert(decoder.Decode(&inst), IsNil)
		if r.Host == "42" {
			c.Check(inst.Services, DeepEquals, []string{"service2.service"})
		} else if r.Host == "1000" {
			c.Check(inst.Services, DeepEquals, []string{"service1.service"})
		} else {
			c.FailNow()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	startFailures, stopFailures, err := s.cli.ServicesStart(
		context.Background(),
		[]string{"service1.service", "service2.service"},
		client.ClientServicesStartOptions{
			DisabledServices: map[int][]string{
				42:   {"service1.service"},
				1000: {"service2.service"},
			},
		},
	)
	c.Assert(err, IsNil)
	c.Check(startFailures, HasLen, 0)
	c.Check(stopFailures, HasLen, 0)
	c.Check(atomic.LoadInt32(&n), Equals, int32(2))
}

func (s *clientSuite) TestServicesStartFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to start services",
    "value": {
      "start-errors": {
        "service2.service": "failed to start"
      }
    }
  }
}`))
	})
	startFailures, stopFailures, err := s.cli.ServicesStart(context.Background(), []string{"service1.service", "service2.service"}, client.ClientServicesStartOptions{})
	c.Assert(err, ErrorMatches, "failed to start services")
	c.Check(startFailures, HasLen, 2)
	c.Check(stopFailures, HasLen, 0)

	failure0 := startFailures[0]
	failure1 := startFailures[1]
	if failure0.Uid == 1000 {
		failure0, failure1 = failure1, failure0
	}
	c.Check(failure0, DeepEquals, client.ServiceFailure{
		Uid:     42,
		Service: "service2.service",
		Error:   "failed to start",
	})
	c.Check(failure1, DeepEquals, client.ServiceFailure{
		Uid:     1000,
		Service: "service2.service",
		Error:   "failed to start",
	})
}

func (s *clientSuite) TestServicesStartOneAgentFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Only produce failure from one agent
		if r.Host != "42" {
			w.WriteHeader(200)
			w.Write([]byte(`{"type": "sync","result": null}`))
			return
		}

		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to start services",
    "value": {
      "start-errors": {
        "service2.service": "failed to start"
      }
    }
  }
}`))
	})
	startFailures, stopFailures, err := s.cli.ServicesStart(context.Background(), []string{"service1.service", "service2.service"}, client.ClientServicesStartOptions{})
	c.Assert(err, ErrorMatches, "failed to start services")
	c.Check(startFailures, DeepEquals, []client.ServiceFailure{
		{
			Uid:     42,
			Service: "service2.service",
			Error:   "failed to start",
		},
	})
	c.Check(stopFailures, HasLen, 0)
}

func (s *clientSuite) TestServicesStartBadErrors(c *C) {
	errorValue := "null"
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Only produce failure from one agent
		if r.Host != "42" {
			w.WriteHeader(200)
			w.Write([]byte(`{"type": "sync","result": null}`))
			return
		}

		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprintf(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to stop services",
    "value": %s
  }
}`, errorValue)))
	})

	// Error value is not a map
	errorValue = "[]"
	startFailures, stopFailures, err := s.cli.ServicesStart(context.Background(), []string{"service1.service"}, client.ClientServicesStartOptions{})
	c.Check(err, ErrorMatches, "failed to stop services")
	c.Check(startFailures, HasLen, 0)
	c.Check(stopFailures, HasLen, 0)

	// Error value is a map, but missing start-errors/stop-errors keys
	errorValue = "{}"
	startFailures, stopFailures, err = s.cli.ServicesStart(context.Background(), []string{"service1.service"}, client.ClientServicesStartOptions{})
	c.Check(err, ErrorMatches, "failed to stop services")
	c.Check(startFailures, HasLen, 0)
	c.Check(stopFailures, HasLen, 0)

	// start-errors/stop-errors are not maps
	errorValue = `{
  "start-errors": [],
  "stop-errors": 42
}`
	startFailures, stopFailures, err = s.cli.ServicesStart(context.Background(), []string{"service1.service"}, client.ClientServicesStartOptions{})
	c.Check(err, ErrorMatches, "failed to stop services")
	c.Check(startFailures, HasLen, 0)
	c.Check(stopFailures, HasLen, 0)

	// start-error/stop-error values are not strings
	errorValue = `{
  "start-errors": {
    "service1.service": 42
  },
  "stop-errors": {
    "service1.service": {}
  }
}`
	startFailures, stopFailures, err = s.cli.ServicesStart(context.Background(), []string{"service1.service"}, client.ClientServicesStartOptions{})
	c.Check(err, ErrorMatches, "failed to stop services")
	c.Check(startFailures, HasLen, 0)
	c.Check(stopFailures, HasLen, 0)

	// When some valid service failures are mixed in with bad
	// ones, report the valid failure along with the error
	// message.
	errorValue = `{
  "start-errors": {
    "service1.service": "failure one",
    "service2.service": 42
  }
}`
	startFailures, stopFailures, err = s.cli.ServicesStart(context.Background(), []string{"service1.service"}, client.ClientServicesStartOptions{})
	c.Check(err, ErrorMatches, "failed to stop services")
	c.Check(startFailures, DeepEquals, []client.ServiceFailure{
		{
			Uid:     42,
			Service: "service1.service",
			Error:   "failure one",
		},
	})
	c.Check(stopFailures, HasLen, 0)
}

func (s *clientSuite) TestServicesStop(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	failures, err := s.cli.ServicesStop(context.Background(), []string{"service1.service", "service2.service"}, false)
	c.Assert(err, IsNil)
	c.Check(failures, HasLen, 0)
}

func (s *clientSuite) TestServicesStopFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to stop services",
    "value": {
      "stop-errors": {
        "service2.service": "failed to stop"
      }
    }
  }
}`))
	})
	failures, err := s.cli.ServicesStop(context.Background(), []string{"service1.service", "service2.service"}, false)
	c.Assert(err, ErrorMatches, "failed to stop services")
	c.Check(failures, HasLen, 2)
	failure0 := failures[0]
	failure1 := failures[1]
	if failure0.Uid == 1000 {
		failure0, failure1 = failure1, failure0
	}
	c.Check(failure0, DeepEquals, client.ServiceFailure{
		Uid:     42,
		Service: "service2.service",
		Error:   "failed to stop",
	})
	c.Check(failure1, DeepEquals, client.ServiceFailure{
		Uid:     1000,
		Service: "service2.service",
		Error:   "failed to stop",
	})
}

func (s *clientSuite) TestServicesRestart(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	failures, err := s.cli.ServicesRestart(context.Background(), []string{"service1.service", "service2.service"}, false)
	c.Assert(err, IsNil)
	c.Check(failures, HasLen, 0)
}

func (s *clientSuite) TestServicesRestartFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to restart services",
    "value": {
      "restart-errors": {
        "service2.service": "failed to restart"
      }
    }
  }
}`))
	})
	failures, err := s.cli.ServicesRestart(context.Background(), []string{"service1.service", "service2.service"}, false)
	c.Assert(err, ErrorMatches, "failed to restart services")
	c.Check(failures, HasLen, 2)
	failure0 := failures[0]
	failure1 := failures[1]
	if failure0.Uid == 1000 {
		failure0, failure1 = failure1, failure0
	}
	c.Check(failure0, DeepEquals, client.ServiceFailure{
		Uid:     42,
		Service: "service2.service",
		Error:   "failed to restart",
	})
	c.Check(failure1, DeepEquals, client.ServiceFailure{
		Uid:     1000,
		Service: "service2.service",
		Error:   "failed to restart",
	})
}

func (s *clientSuite) TestServicesRestartWithReload(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	failures, err := s.cli.ServicesRestart(context.Background(), []string{"service1.service", "service2.service"}, true)
	c.Assert(err, IsNil)
	c.Check(failures, HasLen, 0)
}

func (s *clientSuite) TestServicesRestartWithReloadFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to restart or reload services",
    "value": {
      "restart-errors": {
        "service2.service": "failed to restart or reload"
      }
    }
  }
}`))
	})
	failures, err := s.cli.ServicesRestart(context.Background(), []string{"service1.service", "service2.service"}, true)
	c.Assert(err, ErrorMatches, "failed to restart or reload services")
	c.Check(failures, HasLen, 2)
	failure0 := failures[0]
	failure1 := failures[1]
	if failure0.Uid == 1000 {
		failure0, failure1 = failure1, failure0
	}
	c.Check(failure0, DeepEquals, client.ServiceFailure{
		Uid:     42,
		Service: "service2.service",
		Error:   "failed to restart or reload",
	})
	c.Check(failure1, DeepEquals, client.ServiceFailure{
		Uid:     1000,
		Service: "service2.service",
		Error:   "failed to restart or reload",
	})
}

func (s *clientSuite) TestServiceStatus(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": [{
	"active": false,
	"daemon": "notify",
	"enabled": true,
	"id": "snap.foo.service",
	"installed": true,
	"name": "snap.foo.service",
	"names": ["snap.foo.service"],
	"needs-reload": false
  }]
}`))
	})
	si, failures, err := s.cli.ServiceStatus(context.Background(), []string{"snap.foo.service"})
	c.Assert(err, IsNil)
	c.Check(failures, HasLen, 0)
	c.Check(si, DeepEquals, map[int][]client.ServiceUnitStatus{
		42: {
			{
				Daemon:           "notify",
				Id:               "snap.foo.service",
				Name:             "snap.foo.service",
				Names:            []string{"snap.foo.service"},
				Enabled:          true,
				Active:           false,
				Installed:        true,
				NeedDaemonReload: false,
			},
		},
		1000: {
			{
				Daemon:           "notify",
				Id:               "snap.foo.service",
				Name:             "snap.foo.service",
				Names:            []string{"snap.foo.service"},
				Enabled:          true,
				Active:           false,
				Installed:        true,
				NeedDaemonReload: false,
			},
		},
	})
}

func (s *clientSuite) TestServiceStatusFatalError(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "message": "something bad happened"
  }
}`))
	})
	_, failures, err := s.cli.ServiceStatus(context.Background(), []string{"snap.foo.service"})
	c.Check(err, ErrorMatches, `something bad happened`)
	c.Check(failures, HasLen, 0)
}

func (s *clientSuite) TestServiceStatusWrongResultType(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": ["a", "list"]
}`))
	})
	_, failures, err := s.cli.ServiceStatus(context.Background(), []string{"snap.foo.service"})
	c.Check(failures, DeepEquals, map[int][]client.ServiceFailure{})
	c.Check(err, ErrorMatches, `json: cannot unmarshal string into Go value of type client.ServiceUnitStatus`)
}

func (s *clientSuite) TestServiceStatusFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-status",
    "message": "failed to restart or reload services",
    "value": {
      "status-errors": {
        "service2.service": "failed to restart or reload"
      }
    }
  }
}`))
	})

	si, failures, err := s.cli.ServiceStatus(context.Background(), []string{"service1.service", "service2.service"})
	c.Check(err, IsNil)
	c.Check(si, DeepEquals, map[int][]client.ServiceUnitStatus{})
	c.Check(failures, HasLen, 2)
	for uid, fails := range failures {
		failure0 := fails[0]
		c.Check(failure0, DeepEquals, client.ServiceFailure{
			Uid:     uid,
			Service: "service2.service",
			Error:   "failed to restart or reload",
		})
	}
}

func (s *clientSuite) TestPendingRefreshNotification(c *C) {
	var n int32
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		c.Assert(r.URL.Path, Equals, "/v1/notifications/pending-refresh")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"type": "sync"}`))
	})
	err := s.cli.PendingRefreshNotification(context.Background(), &client.PendingSnapRefreshInfo{})
	c.Assert(err, IsNil)
	c.Check(atomic.LoadInt32(&n), Equals, int32(2))
}

func (s *clientSuite) TestFinishRefreshNotification(c *C) {
	var n int32
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		c.Assert(r.URL.Path, Equals, "/v1/notifications/finish-refresh")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(string(body), DeepEquals, `{"instance-name":"some-snap"}`)
	})
	err := s.cli.FinishRefreshNotification(context.Background(), &client.FinishedSnapRefreshInfo{InstanceName: "some-snap"})
	c.Assert(err, IsNil)
	// two calls because clientSuite simulates two user sessions (two
	// snapd-session-agent.socket sockets).
	c.Check(atomic.LoadInt32(&n), Equals, int32(2))
}

func (s *clientSuite) TestPendingRefreshNotificationOneClient(c *C) {
	cli := client.NewForUids(1000)
	var n int32
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		c.Assert(r.URL.Path, Equals, "/v1/notifications/pending-refresh")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"type": "sync"}`))
	})
	err := cli.PendingRefreshNotification(context.Background(), &client.PendingSnapRefreshInfo{})
	c.Assert(err, IsNil)
	c.Check(atomic.LoadInt32(&n), Equals, int32(1))
}

func (s *clientSuite) TestAppsKill(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Equals, "/v1/app-control")
		w.Header().Set("Content-Type", "application/json")

		decoder := json.NewDecoder(r.Body)
		var inst client.AppInstruction
		c.Assert(decoder.Decode(&inst), IsNil)
		c.Check(inst.Action, Equals, "kill")
		c.Check(inst.Snaps, DeepEquals, []string{"foo", "foo_bar"})
		c.Check(inst.Signal, Equals, syscall.SIGKILL)
		c.Check(inst.Reason, Equals, snap.KillReasonForceRemove)

		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	failures, err := s.cli.AppsKill(context.Background(), []string{"foo", "foo_bar"}, 9, snap.KillReasonForceRemove)
	c.Assert(err, IsNil)
	c.Check(failures, HasLen, 0)
}

func (s *clientSuite) TestAppsKillFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Equals, "/v1/app-control")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "app-control",
    "message": "failed to kill running apps",
    "value": {
      "kill-errors": {
        "snap.foo.some-app-7414e1a3-6d08-43ff-a81c-6547242a78b0.scope": "failed to kill running app"
      }
    }
  }
}`))
	})
	failures, err := s.cli.AppsKill(context.Background(), []string{"foo", "foo_bar"}, 9, snap.KillReasonForceRemove)
	c.Assert(err, ErrorMatches, "failed to kill running apps")
	c.Check(failures, HasLen, 2)
	failure0 := failures[0]
	failure1 := failures[1]
	if failure0.Uid == 1000 {
		failure0, failure1 = failure1, failure0
	}
	c.Check(failure0, DeepEquals, client.AppFailure{
		Uid:   42,
		Unit:  "snap.foo.some-app-7414e1a3-6d08-43ff-a81c-6547242a78b0.scope",
		Error: "failed to kill running app",
	})
	c.Check(failure1, DeepEquals, client.AppFailure{
		Uid:   1000,
		Unit:  "snap.foo.some-app-7414e1a3-6d08-43ff-a81c-6547242a78b0.scope",
		Error: "failed to kill running app",
	})
}

func (s *clientSuite) TestAppsKillBadErrors(c *C) {
	errorValue := "null"
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Only produce failure from one agent
		if r.Host != "42" {
			w.WriteHeader(200)
			w.Write([]byte(`{"type": "sync","result": null}`))
			return
		}

		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprintf(`{
  "type": "error",
  "result": {
    "kind": "app-control",
    "message": "failed to kill running apps",
    "value": %s
  }
}`, errorValue)))
	})

	// Error value is not a map
	errorValue = "[]"
	failures, err := s.cli.AppsKill(context.Background(), []string{"foo"}, 9, snap.KillReasonForceRemove)
	c.Check(err, ErrorMatches, "failed to kill running apps")
	c.Check(failures, HasLen, 0)

	// Error value is a map, but missing kill-errors key
	errorValue = "{}"
	failures, err = s.cli.AppsKill(context.Background(), []string{"foo"}, 9, snap.KillReasonForceRemove)
	c.Check(err, ErrorMatches, "failed to kill running apps")
	c.Check(failures, HasLen, 0)

	// kill-errors is a map
	errorValue = `{
  "kill-errors": []
}`
	failures, err = s.cli.AppsKill(context.Background(), []string{"foo"}, 9, snap.KillReasonForceRemove)
	c.Check(err, ErrorMatches, "failed to kill running apps")
	c.Check(failures, HasLen, 0)

	// kill-error values are not strings
	errorValue = `{
  "kill-errors": {
    "snap.foo.some-app-7414e1a3-6d08-43ff-a81c-6547242a78b0.scope": 42
  }
}`
	failures, err = s.cli.AppsKill(context.Background(), []string{"foo"}, 9, snap.KillReasonForceRemove)
	c.Check(err, ErrorMatches, "failed to kill running apps")
	c.Check(failures, HasLen, 0)

	// When some valid app failures are mixed in with bad
	// ones, report the valid failure along with the error
	// message.
	errorValue = `{
  "kill-errors": {
    "snap.foo.some-app-7414e1a3-6d08-43ff-a81c-6547242a78b0.scope": "failure one",
    "snap.foo.some-other-app-f3a1d6fa-c660-4b7d-a450-aaa8849614c7.scope": 42
  }
}`
	failures, err = s.cli.AppsKill(context.Background(), []string{"foo"}, 9, snap.KillReasonForceRemove)
	c.Check(err, ErrorMatches, "failed to kill running apps")
	c.Check(failures, HasLen, 1)
	c.Check(failures, DeepEquals, []client.AppFailure{
		{
			Uid:   42,
			Unit:  "snap.foo.some-app-7414e1a3-6d08-43ff-a81c-6547242a78b0.scope",
			Error: "failure one",
		},
	})
}
