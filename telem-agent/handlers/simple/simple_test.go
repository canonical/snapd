// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package simple_test

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"os"
	"testing"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/canonical/telem-agent/handlers/simple"
	"github.com/canonical/telem-agent/pkg/session"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type simpleSuite struct {
	topic     string
	payload   []byte
	userProps []packets.User
	handler   simple.Handler
	ctx       context.Context
}

var _ = Suite(&simpleSuite{})

func (s *simpleSuite) SetUpSuite(c *C) {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	s.topic = "test/topic"
	s.payload = []byte("Hello World")
	s.handler = *simple.New(slog.New(logHandler))
	s.ctx = session.NewContext(context.Background(), &session.Session{})
	s.userProps = []packets.User{{Key: "version", Value: "3.1.1"}}
}

func (ss *simpleSuite) TestAuthConnect(c *C) {
	err := ss.handler.AuthConnect(ss.ctx, false, nil)
	c.Check(err, IsNil)
}

func (ss *simpleSuite) TestAuthPublish(c *C) {
	err := ss.handler.AuthPublish(ss.ctx, &ss.topic, &ss.payload, &ss.userProps)
	c.Assert(err, IsNil)

	payloadAsString := string(ss.payload)
	c.Check(payloadAsString, Equals, "Hello World")
	c.Check(ss.userProps[0].Key, Equals, "version")
	c.Check(ss.userProps[0].Value, Equals, "3.1.1")
}

func (ss *simpleSuite) TestAuthSubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: ss.topic}}
	err := ss.handler.AuthSubscribe(ss.ctx, &subs, nil)
	c.Assert(err, IsNil)

	c.Check(ss.topic, Equals, "test/topic")
}

func (ss *simpleSuite) TestAuthUnsubscribe(c *C) {
	err := ss.handler.AuthUnsubscribe(ss.ctx, &[]string{ss.topic}, nil)
	c.Assert(err, IsNil)

	c.Check(ss.topic, Equals, "test/topic")
}

func (ss *simpleSuite) TestDownSubscribe(c *C) {
	err := ss.handler.DownPublish(ss.ctx, &ss.topic, nil)

	c.Assert(err, IsNil)

	c.Check(ss.topic, Equals, "test/topic")
}

func (ss *simpleSuite) TestConnect(c *C) {
	err := ss.handler.Connect(ss.ctx)

	c.Assert(err, IsNil)
}

func (ss *simpleSuite) TestPublish(c *C) {
	err := ss.handler.Publish(ss.ctx, &ss.topic, &ss.payload)
	c.Assert(err, IsNil)

	payloadAsString := string(ss.payload)
	c.Check(payloadAsString, Equals, "Hello World")
}

func (ss *simpleSuite) TestSubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: ss.topic}}
	err := ss.handler.Subscribe(ss.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(ss.topic, Equals, "test/topic")
}

func (ss *simpleSuite) TestUnsubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: ss.topic}}
	err := ss.handler.Unsubscribe(ss.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(ss.topic, Equals, "test/topic")
}

func (ss *simpleSuite) TestDisconnect(c *C) {
	err := ss.handler.Disconnect(ss.ctx)

	c.Assert(err, IsNil)
}

func (ss *simpleSuite) TestNoSession(c *C) {
	err := ss.handler.AuthConnect(context.Background(), false, nil)
	c.Check(err, NotNil)
}

func (ss *simpleSuite) TestAuthConnectWithCertificate(c *C) {
	fakeCertificate := x509.Certificate{Subject: pkix.Name{CommonName: "fake_cert"}}
	ctxWithCertificate := session.NewContext(context.Background(), &session.Session{Cert: fakeCertificate})

	err := ss.handler.AuthConnect(ctxWithCertificate, false, nil)
	c.Check(err, IsNil)
}
