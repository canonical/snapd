// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

const (
	appID = "chatroom.ogra"
)

func (cs *clientSuite) TestClientServicesCallsEndpoint(c *C) {
	_, _ = cs.cli.Services(appID)
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.URL.Path, Equals, fmt.Sprintf("/v2/snaps/%s/services", appID))
}

func (cs *clientSuite) TestClientServices(c *C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"chatroom": {
				"op": "status",
				"spec": {
					"name": "chatroom",
					"description": "A simple WebRTC videochat",
					"start": "start-service.sh",
					"stop-timeout": "30s",
					"ports": {
						"external": {
							"ui": {
								"Port": "6565/tcp",
								"Negotiable": false
							}
						}
					}
				},
				"status": {
					"service_file_name": "chatroom_chatroom_0.1-8.service",
					"load_state": "loaded",
					"active_state": "active",
					"sub_state": "running",
					"unit_file_state": "enabled",
					"snap_name": "chatroom",
					"service_name": "chatroom"
				}
			}
		}
	}`
	services, err := cs.cli.Services(appID)
	c.Check(err, IsNil)
	c.Check(services, DeepEquals, map[string]*client.Service{
		"chatroom": &client.Service{
			Op: "status",
			Spec: client.ServiceSpec{
				Name:        "chatroom",
				Description: "A simple WebRTC videochat",
				Start:       "start-service.sh",
				StopTimeout: "30s",
				Ports: client.ServicePorts{
					External: map[string]client.ServicePort{
						"ui": client.ServicePort{
							Port:       "6565/tcp",
							Negotiable: false,
						},
					},
				},
			},
			Status: client.ServiceStatus{
				ServiceFileName: "chatroom_chatroom_0.1-8.service",
				LoadState:       "loaded",
				ActiveState:     "active",
				SubState:        "running",
				UnitFileState:   "enabled",
				SnapName:        "chatroom",
				AppName:         "chatroom",
			},
		},
	})
}
