// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0
package mqtt_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"

	"github.com/canonical/mqtt.golang/mock/testserver"
	"github.com/canonical/telem-agent/config"
	"github.com/canonical/telem-agent/pkg/mqtt"
	"github.com/canonical/telem-agent/pkg/session"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type mqttSuite struct {
	interceptor session.Interceptor
	handler     session.Handler
	logger      slog.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	proxy       mqtt.Proxy
	config      config.Config
	server      testserver.Instance
}

var _ = Suite(&mqttSuite{})

func (mq *mqttSuite) SetUpSuite(c *C) {
	mq.ctx, mq.cancel = context.WithCancel(context.Background())

	mq.config = config.Config{Address: ":1883", PathPrefix: "", Target: ":1884"}

	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	mq.logger = *slog.New(logHandler)

	var logger testserver.Logger
	mq.server = *testserver.New(logger)
}

func (mq *mqttSuite) TestCreatingNewProxy(c *C) {
	mq.proxy = *mqtt.New(mq.config, mq.handler, mq.interceptor, &mq.logger)

	c.Check(mq.proxy, NotNil)
}

func (mq *mqttSuite) TestListenNoConfig(c *C) {
	emptyProxy := *mqtt.New(config.Config{}, mq.handler, mq.interceptor, &mq.logger)
	err := emptyProxy.Listen(mq.ctx)

	c.Check(err, NotNil)
}

func (mq *mqttSuite) TestListenBadConfig(c *C) {
	emptyProxy := *mqtt.New(config.Config{Address: "RNADOM"}, mq.handler, mq.interceptor, &mq.logger)
	err := emptyProxy.Listen(mq.ctx)

	c.Check(err, NotNil)
}

func (mq *mqttSuite) TestListenGoodConfig(c *C) {
	in, out, addr, _ := netPipe(context.Background())
	emptyProxy := *mqtt.New(config.Config{Address: addr}, mq.handler, mq.interceptor, &mq.logger)
	go emptyProxy.Listen(mq.ctx)

	in.Close()
	out.Close()

	mq.cancel()

	// c.Check(err, IsNil)
}

func (mq *mqttSuite) TestListenBadConn(c *C) {
	in, _, addr, _ := netPipe(context.Background())
	emptyProxy := *mqtt.New(config.Config{Address: addr}, mq.handler, mq.interceptor, &mq.logger)
	go emptyProxy.Listen(mq.ctx)

	in.Write([]byte("Hello"))
	// mq.cancel()
}

func netPipe(ctx context.Context) (net.Conn, net.Conn, string, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0") // Port 0 is wildcard port; OS will choose port for us
	if err != nil {
		return nil, nil, "", err
	}
	defer l.Close()
	var d net.Dialer
	userCon, err := d.DialContext(ctx, "tcp", l.Addr().String()) // Dial the port we just listened on
	if err != nil {
		return nil, nil, "", err
	}
	ourCon, err := l.Accept() // Should return immediately
	if err != nil {
		userCon.Close()
		return nil, nil, "", err
	}
	return userCon, ourCon, l.Addr().String(), nil
}
