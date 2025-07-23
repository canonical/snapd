// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package injector_test

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/snapcore/snapd/telemagent/handlers/injector"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type injectorSuite struct {
	topic     string
	payload   []byte
	userProps []packets.User
	handler   injector.Injector
	ctx       context.Context
}

var _ = Suite(&injectorSuite{})

func (inj *injectorSuite) SetUpSuite(c *C) {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	inj.topic = "test/topic"

	inj.payload = []byte("Hello World")
	inj.handler = *injector.New(slog.New(logHandler), "Telem team is awesome")
	inj.ctx = session.NewContext(context.Background(), &session.Session{})
}

func (inj *injectorSuite) TestAuthConnect(c *C) {
	err := inj.handler.AuthConnect(inj.ctx, false, nil, nil, nil)
	c.Check(err, IsNil)
}

func (inj *injectorSuite) TestAuthPublish(c *C) {
	hostName, _ := os.Hostname()
	err := inj.handler.AuthPublish(inj.ctx, &inj.topic, &inj.payload, &inj.userProps)
	c.Assert(err, IsNil)

	c.Check(inj.topic, Equals, "test/topic")

	expectedPayload := fmt.Sprintf("Hello World Telem team is awesome, Device: %s", hostName)
	c.Check(string(inj.payload), Equals, expectedPayload)
}

func (inj *injectorSuite) TestAuthSubscribe(c *C) {
	sub := packets.SubOptions{Topic: inj.topic}
	err := inj.handler.AuthSubscribe(inj.ctx, &[]packets.SubOptions{sub}, &inj.userProps)
	c.Assert(err, IsNil)

	c.Check(sub.Topic, Equals, "test/topic")
}

func (inj *injectorSuite) TestAuthUnsubscribe(c *C) {
	err := inj.handler.AuthUnsubscribe(inj.ctx, &[]string{inj.topic}, &inj.userProps)
	c.Assert(err, IsNil)

	c.Check(inj.topic, Equals, "test/topic")
}

func (inj *injectorSuite) TestDownPublish(c *C) {
	translatedTopic := "test/topic"
	err := inj.handler.DownPublish(inj.ctx, &translatedTopic, &inj.userProps)
	c.Assert(err, IsNil)

	c.Check(translatedTopic, Equals, "test/topic")
}

func (inj *injectorSuite) TestConnect(c *C) {
	err := inj.handler.Connect(inj.ctx)

	c.Assert(err, IsNil)
}

func (inj *injectorSuite) TestPublish(c *C) {
	basePayload := []byte("Hello World")
	err := inj.handler.Publish(inj.ctx, &inj.topic, &basePayload)
	c.Assert(err, IsNil)

	payloadAsString := string(basePayload)
	c.Check(payloadAsString, Equals, "Hello World")
}

func (inj *injectorSuite) TestSubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: inj.topic}}
	err := inj.handler.Subscribe(inj.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(inj.topic, Equals, "test/topic")
}

func (inj *injectorSuite) TestUnsubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: inj.topic}}
	err := inj.handler.Unsubscribe(inj.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(inj.topic, Equals, "test/topic")
}

func (inj *injectorSuite) TestDisconnect(c *C) {
	err := inj.handler.Disconnect(inj.ctx)

	c.Assert(err, IsNil)
}

func (inj *injectorSuite) TestNoSession(c *C) {
	err := inj.handler.AuthConnect(context.Background(), false, nil, nil, nil)
	c.Check(err, NotNil)
}

func (inj *injectorSuite) TestAuthConnectWithCertificate(c *C) {
	fakeCertificate := x509.Certificate{Subject: pkix.Name{CommonName: "fake_cert"}}
	ctxWithCertificate := session.NewContext(context.Background(), &session.Session{Cert: fakeCertificate})

	err := inj.handler.AuthConnect(ctxWithCertificate, false, nil, nil, nil)
	c.Check(err, IsNil)
}
