// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package translator_test

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"os"
	"testing"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/snapcore/snapd/telemagent/handlers/translator"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type translatorSuite struct {
	topic               string
	payload             []byte
	handler             translator.Translator
	topicTranslation    map[string]string
	revTopicTranslation map[string]string
	userProps           []packets.User
	ctx                 context.Context
}

var _ = Suite(&translatorSuite{})

func (ts *translatorSuite) SetUpSuite(c *C) {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	ts.topic = "test/topic"

	ts.topicTranslation = make(map[string]string)
	ts.topicTranslation["test/topic"] = "test/foo"

	ts.revTopicTranslation = make(map[string]string)
	ts.revTopicTranslation["test/foo"] = "test/topic"

	ts.payload = []byte("Hello World")
	ts.userProps = []packets.User{{Key: "version", Value: "3.1.1"}}

	ts.handler = *translator.New(slog.New(logHandler), ts.topicTranslation, ts.revTopicTranslation)
	ts.ctx = session.NewContext(context.Background(), &session.Session{})
}

func (ts *translatorSuite) TestAuthConnect(c *C) {
	err := ts.handler.AuthConnect(ts.ctx, false, nil)
	c.Check(err, IsNil)
}

func (ts *translatorSuite) TestAuthPublishTopicFound(c *C) {
	err := ts.handler.AuthPublish(ts.ctx, &ts.topic, &ts.payload, &ts.userProps)
	c.Assert(err, IsNil)

	c.Check(ts.topic, Equals, "test/foo")
}

func (ts *translatorSuite) TestAuthPublishTopicNotFound(c *C) {
	unlistedTopic := "test/bar"
	err := ts.handler.AuthPublish(ts.ctx, &unlistedTopic, &ts.payload, nil)
	c.Assert(err, IsNil)

	c.Check(unlistedTopic, Equals, "test/bar")
}

func (ts *translatorSuite) TestAuthSubscribeTopicFound(c *C) {
	subs := []packets.SubOptions{{Topic: "test/topic"}}
	err := ts.handler.AuthSubscribe(ts.ctx, &subs, nil)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, "test/foo")
}

func (ts *translatorSuite) TestAuthSubscribeTopicNotFound(c *C) {
	sub := packets.SubOptions{Topic: "test/bar"}
	err := ts.handler.AuthSubscribe(ts.ctx, &[]packets.SubOptions{sub}, nil)
	c.Assert(err, IsNil)

	c.Check(sub.Topic, Equals, "test/bar")
}

func (ts *translatorSuite) TestAuthUnsubscribe(c *C) {
	err := ts.handler.AuthUnsubscribe(ts.ctx, &[]string{ts.topic}, nil)
	c.Assert(err, IsNil)

	c.Check(ts.topic, Equals, "test/foo")
}

func (ts *translatorSuite) TestDownPublishTopicFound(c *C) {
	translatedTopic := "test/foo"
	err := ts.handler.DownPublish(ts.ctx, &translatedTopic, nil)
	c.Assert(err, IsNil)

	c.Check(translatedTopic, Equals, "test/topic")
}

func (ts *translatorSuite) TestDownPublishTopicNotFound(c *C) {
	translatedTopic := "test/bar"
	err := ts.handler.DownPublish(ts.ctx, &translatedTopic, nil)
	c.Assert(err, IsNil)

	c.Check(translatedTopic, Equals, "test/bar")
}

func (ts *translatorSuite) TestConnect(c *C) {
	err := ts.handler.Connect(ts.ctx)

	c.Assert(err, IsNil)
}

func (ts *translatorSuite) TestPublish(c *C) {
	err := ts.handler.Publish(ts.ctx, &ts.topic, &ts.payload)
	c.Assert(err, IsNil)

	payloadAsString := string(ts.payload)
	c.Check(payloadAsString, Equals, "Hello World")
}

func (ts *translatorSuite) TestSubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: ts.topic}}
	err := ts.handler.Subscribe(ts.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(ts.topic, Equals, "test/foo")
}

func (ts *translatorSuite) TestUnsubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: ts.topic}}
	err := ts.handler.Unsubscribe(ts.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(ts.topic, Equals, "test/foo")
}

func (ts *translatorSuite) TestDisconnect(c *C) {
	err := ts.handler.Disconnect(ts.ctx)

	c.Assert(err, IsNil)
}

func (ts *translatorSuite) TestNoSession(c *C) {
	err := ts.handler.AuthConnect(context.Background(), false, nil)
	c.Check(err, NotNil)
}

func (ts *translatorSuite) TestAuthConnectWithCertificate(c *C) {
	fakeCertificate := x509.Certificate{Subject: pkix.Name{CommonName: "fake_cert"}}
	ctxWithCertificate := session.NewContext(context.Background(), &session.Session{Cert: fakeCertificate})

	err := ts.handler.AuthConnect(ctxWithCertificate, false, nil)
	c.Check(err, IsNil)
}
