// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0
package rest_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/canonical/mqtt.golang/paho"
	"github.com/canonical/telem-agent/internal/utils"
	"github.com/canonical/telem-agent/pkg/rest"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type restSuite struct {
	ctx        context.Context
	Endpoint string
	logger     *slog.Logger
	clientConn net.Conn
	client     *paho.Client
	done       chan struct{}
}

var _ = Suite(&restSuite{})

func (ss *restSuite) SetUpSuite(c *C) {
	ss.ctx = context.Background()

	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	ss.logger = slog.New(logHandler)
}

func (ss *restSuite) SetUpTest(c *C) {
	
	ss.Endpoint = "localhost:1883"

	conn, err := net.Dial("tcp", ss.Endpoint)
    if err != nil {
        panic(err)
    }

	ss.client = paho.NewClient(paho.ClientConfig{
		Conn: conn,
	})


}

func sendResponse(ctx context.Context, responseTopic string, client *paho.Client) error {
	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
	}

	_, err := client.Connect(ctx, cp)
	if err != nil {
		return err
	}

	deviceID, err := utils.GetDeviceId()
	if err != nil {
		return err
	}	

	s := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: fmt.Sprintf("/%s/canonical/landscape/request", deviceID), QoS: 2},
		},
	}

	_, err = client.Subscribe(ctx, s)
	if err != nil {
		return err
	}

	p := &paho.Publish{
		Topic:   responseTopic,
		QoS:     2,
		Payload: []byte("test payload"),
	}
	_, err = client.Publish(ctx, p)
	if err != nil {
		return err
	}

	return nil
}

func (ss *restSuite) TestValidEndpoint(c *C) {
	_, err := rest.NewServer(rest.Config{Endpoint: "localhost:1883"}, nil, nil, nil)

	c.Check(err, IsNil)
}

func (ss *restSuite) TestSendingResponse(c *C) {
	server, err := rest.NewServer(rest.Config{Endpoint: "mqtt://"+ss.Endpoint}, ss.logger, mockSnapGetInfo, nil)
	c.Assert(err, IsNil)
	

	err = rest.MockStartServer(ss.ctx, server)
	if err != nil {
		panic(err)
	}

	deviceID, err := utils.GetDeviceId()
	if err != nil {
		panic(err)
	}	


	req := httptest.NewRequest(http.MethodGet, "localhost:9090/landscape", nil)
	req.URL.Path = "localhost:9090/landscape"

	
	w := httptest.NewRecorder()

	go rest.MockEchoHandler(w, req, server)
	go sendResponse(ss.ctx, fmt.Sprintf("/%s/canonical/landscape/response", deviceID), ss.client)
	
	res := w.Result()
	defer res.Body.Close()
	
	data, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	c.Check(res.StatusCode, Equals, http.StatusBadRequest)
	c.Assert(data, IsNil)

	ss.done <- struct{}{}
}

func mockSnapGetInfo(addr string) (string, string, error) {
	return "canonical", "multipass", nil
}