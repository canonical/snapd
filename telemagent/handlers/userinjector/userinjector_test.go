// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package userinjector_test

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"os"
	"testing"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/snapcore/snapd/telemagent/handlers/userinjector"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type userinjectorSuite struct {
	topic     string
	payload   []byte
	userProps []packets.User
	handler   userinjector.UserInjector
	ctx       context.Context
}

var _ = Suite(&userinjectorSuite{})

func (us *userinjectorSuite) SetUpSuite(c *C) {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	us.topic = "test/topic"

	us.payload = []byte("Hello World")
	us.handler = *userinjector.New(slog.New(logHandler))
	us.ctx = session.NewContext(context.Background(), &session.Session{})
}

func (us *userinjectorSuite) TestAuthConnect(c *C) {
	err := us.handler.AuthConnect(us.ctx, false, nil, nil, nil)
	c.Check(err, IsNil)
}

func (us *userinjectorSuite) TestAuthPublish(c *C) {
	hostName, _ := os.Hostname()
	err := us.handler.AuthPublish(us.ctx, &us.topic, &us.payload, &us.userProps)
	c.Assert(err, IsNil)

	c.Check(us.topic, Equals, "test/topic")
	c.Check(string(us.payload), Equals, "Hello World")
	c.Check(us.userProps[0], Equals, packets.User{Key: "Device", Value: hostName})
}

func (us *userinjectorSuite) TestAuthSubscribe(c *C) {
	sub := packets.SubOptions{Topic: us.topic}
	err := us.handler.AuthSubscribe(us.ctx, &[]packets.SubOptions{sub}, &us.userProps)
	c.Assert(err, IsNil)

	c.Check(sub.Topic, Equals, "test/topic")
}

func (us *userinjectorSuite) TestAuthUnsubscribe(c *C) {
	err := us.handler.AuthUnsubscribe(us.ctx, &[]string{us.topic}, &us.userProps)
	c.Assert(err, IsNil)

	c.Check(us.topic, Equals, "test/topic")
}

func (us *userinjectorSuite) TestDownPublish(c *C) {
	translatedTopic := "test/topic"
	err := us.handler.DownPublish(us.ctx, &translatedTopic, &us.userProps)
	c.Assert(err, IsNil)

	c.Check(translatedTopic, Equals, "test/topic")
}

func (us *userinjectorSuite) TestConnect(c *C) {
	err := us.handler.Connect(us.ctx)

	c.Assert(err, IsNil)
}

func (us *userinjectorSuite) TestPublish(c *C) {
	err := us.handler.Publish(us.ctx, &us.topic, &us.payload)
	c.Assert(err, IsNil)

	payloadAsString := string(us.payload)
	c.Check(payloadAsString, Equals, "Hello World")
}

func (us *userinjectorSuite) TestSubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: us.topic}}
	err := us.handler.Subscribe(us.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(us.topic, Equals, "test/topic")
}

func (us *userinjectorSuite) TestUnsubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: us.topic}}
	err := us.handler.Unsubscribe(us.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(us.topic, Equals, "test/topic")
}

func (us *userinjectorSuite) TestDisconnect(c *C) {
	err := us.handler.Disconnect(us.ctx)

	c.Assert(err, IsNil)
}

func (us *userinjectorSuite) TestNoSession(c *C) {
	err := us.handler.AuthConnect(context.Background(), false, nil, nil, nil)
	c.Check(err, NotNil)
}

func (us *userinjectorSuite) TestAuthConnectWithCertificate(c *C) {
	fakeCertificate := x509.Certificate{Subject: pkix.Name{CommonName: "fake_cert"}}
	ctxWithCertificate := session.NewContext(context.Background(), &session.Session{Cert: fakeCertificate})

	err := us.handler.AuthConnect(ctxWithCertificate, false, nil, nil, nil)
	c.Check(err, IsNil)
}
