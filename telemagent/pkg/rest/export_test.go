// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package rest

import (
	"context"
	"net/http"

	"github.com/canonical/mqtt.golang/autopaho"
)

func MockEchoHandler(writer http.ResponseWriter, request *http.Request, s *Server) {
	s.echoHandler(writer, request)
}

func MockStartServer(ctx context.Context,  s *Server) error {
	c, err := autopaho.NewConnection(ctx, s.mqttConfig) // starts process; will reconnect until context cancelled
	if err != nil {
		return err
	}

	s.mqttClient = c
	s.ctx = ctx
	// Wait for the connection to come up
	if err = s.mqttClient.AwaitConnection(ctx); err != nil {
		return err
	}

	return nil
}