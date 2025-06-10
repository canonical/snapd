// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package snapadder_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/snapcore/snapd/telemagent/handlers/snapadder"
	"github.com/snapcore/snapd/telemagent/interceptors/permissioncontroller"
	"github.com/snapcore/snapd/telemagent/internal/utils"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/strutil"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

const syncResp = `{
	"type": "sync",
	"status-code": 200,
	"status": "OK",
	"result": %s
  }`

type snapadderSuite struct {
	topic      string
	payload    []byte
	userProps  []packets.User
	handler    snapadder.SnapAdder
	ctx        context.Context
	snapPID    int
	nonSnapPID int
	snapClient *client.Client
}

var _ = Suite(&snapadderSuite{})

const sampleTopic = "test/topic"

func (sa *snapadderSuite) SetUpSuite(c *C) {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	sa.topic = sampleTopic

	sa.payload = []byte("Hello World")
	sa.handler = *snapadder.New(slog.New(logHandler))
	sa.ctx = session.NewContext(context.Background(), &session.Session{})

	cmd := exec.Command("pgrep", "-n", "multipass")
	out, _ := cmd.Output()
	out = bytes.TrimSuffix(out, []byte("\n"))
	test_pid, _ := strconv.Atoi(string(out))

	cmd2 := exec.Command("pgrep", "-n", "ping")
	out2, _ := cmd2.Output()
	out2 = bytes.TrimSuffix(out2, []byte("\n"))
	test_pid2, _ := strconv.Atoi(string(out2))

	sa.snapPID = test_pid
	sa.nonSnapPID = test_pid2

	sa.snapClient = client.New(nil)
}

func (sa *snapadderSuite) SetUpTest(c *C) {
	sa.topic = sampleTopic
}

func (sa *snapadderSuite) TestAuthConnect(c *C) {
	err := sa.handler.AuthConnect(sa.ctx, false, nil)
	c.Check(err, IsNil)
}

func (sa *snapadderSuite) TestAuthConnectWithWill(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	lastWillTopic := "my-topic/hello"

	deviceId, err := utils.GetDeviceId()
	if err != nil {
		panic(err)
	}

	newTopic := fmt.Sprintf("/%s/canonical/my-topic/hello", deviceId)

	err = sa.handler.AuthConnect(ctxWithProc, true, &lastWillTopic)
	c.Check(err, IsNil)
	c.Check(lastWillTopic, Equals, newTopic)
}

func (sa *snapadderSuite) TestAuthPublishWithNoPID(c *C) {
	err := sa.handler.AuthPublish(sa.ctx, &sa.topic, &sa.payload, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(sa.topic, Equals, sampleTopic)

	c.Check(string(sa.payload), Equals, "Hello World")
}

func (sa *snapadderSuite) TestAuthPublishWithSnapPID(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	err := sa.handler.AuthPublish(ctxWithProc, &sa.topic, &sa.payload, &sa.userProps)
	c.Assert(err, IsNil)

	deviceId, err := utils.GetDeviceId()
	if err != nil {
		panic(err)
	}

	newTopic := fmt.Sprintf("/%s/canonical/test/topic", deviceId)

	c.Check(sa.topic, Equals, newTopic)

	c.Check(string(sa.payload), Equals, "Hello World")
}

func (sa *snapadderSuite) TestAuthPublishWithDollar(c *C) {
	dollarTopic := "$test/topic"
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	err := sa.handler.AuthPublish(ctxWithProc, &dollarTopic, &sa.payload, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(dollarTopic, Equals, permissioncontroller.DeniedTopic)

	c.Check(string(sa.payload), Equals, "Hello World")
}

func (sa *snapadderSuite) TestAuthPublishWithGlobalTopicSamePublisher(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	globalTopic := "/device/canonical/test/topic"
	err := sa.handler.AuthPublish(ctxWithProc, &globalTopic, &sa.payload, &sa.userProps)
	c.Assert(err, IsNil)

	c.Assert(globalTopic, Equals, "/device/canonical/test/topic")

	c.Check(string(sa.payload), Equals, "Hello World")
}

func (sa *snapadderSuite) TestAuthPublishWithGlobalTopicNotAllowed(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	globalTopic := "/device/emqx/test/topic"
	err := sa.handler.AuthPublish(ctxWithProc, &globalTopic, &sa.payload, &sa.userProps)
	c.Assert(err, IsNil)

	c.Assert(globalTopic, Equals, "DENIED")

	c.Check(string(sa.payload), Equals, "Hello World")
}

func (sa *snapadderSuite) TestAuthSubscribeWithLocalNamespace(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []packets.SubOptions{{Topic: "my-topic/hello"}}
	err := sa.handler.AuthSubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, "/+/canonical/my-topic/hello")
}

func (sa *snapadderSuite) TestAuthSubscribeWithNoSnapInfo(c *C) {
	subs := []packets.SubOptions{{Topic: sa.topic}}
	err := sa.handler.AuthSubscribe(sa.ctx, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthSubscribeWithDollarSign(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []packets.SubOptions{{Topic: "$share/my-group/test/topic"}}
	err := sa.handler.AuthSubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthSubscribeWithGlobalNamespaceValidName(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []packets.SubOptions{{Topic: "/device-1/canonical/my-topic/hello"}}
	err := sa.handler.AuthSubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, "/device-1/canonical/my-topic/hello")
}

func (sa *snapadderSuite) TestAuthSubscribeWithGlobalNamespaceInvalidName(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []packets.SubOptions{{Topic: "/foo/test/topic"}}
	err := sa.handler.AuthSubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthSubscribeWithGlobalNamespaceInvalidLevelsName(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []packets.SubOptions{{Topic: "/foo"}}
	err := sa.handler.AuthSubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthSubscribeLocalNamespaceDollarSign(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []packets.SubOptions{{Topic: "$hello"}}
	err := sa.handler.AuthSubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0].Topic, Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthunsubscribeWithNoSnapInfo(c *C) {
	topics := []string{sa.topic}
	err := sa.handler.AuthUnsubscribe(sa.ctx, &topics, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(topics[0], Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthUnsubscribeWithSnapInfo(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	topics := []string{sa.topic}
	err := sa.handler.AuthUnsubscribe(ctxWithProc, &topics, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(topics[0], Equals, "/+/canonical/test/topic")
}

func (sa *snapadderSuite) TestAuthUnsubscribeWithDollarSign(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []string{"$share/my-group/test/topic"}
	err := sa.handler.AuthUnsubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0], Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthUnsubscribeWithGlobalNamespaceValidName(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	topics := []string{"/device-1/canonical/my-topic/hello"}
	err := sa.handler.AuthUnsubscribe(ctxWithProc, &topics, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(topics[0], Equals, "/device-1/canonical/my-topic/hello")
}

func (sa *snapadderSuite) TestAuthUnsubscribeWithGlobalNamespaceInvalidLevels(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	topics := []string{"/foo"}
	err := sa.handler.AuthUnsubscribe(ctxWithProc, &topics, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(topics[0], Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestAuthUnsubscribeLocalNamespaceDollarSign(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	subs := []string{"$hello"}
	err := sa.handler.AuthUnsubscribe(ctxWithProc, &subs, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(subs[0], Equals, permissioncontroller.DeniedTopic)
}

func (sa *snapadderSuite) TestDownPublish(c *C) {
	ctxWithProc := session.AddSnapToContext(sa.ctx, "canonical", "multipass")
	deviceID, err := utils.GetDeviceId()
	c.Assert(err, IsNil)

	translatedTopic := fmt.Sprintf("/%s/canonical/topic", deviceID)
	err = sa.handler.DownPublish(ctxWithProc, &translatedTopic, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(translatedTopic, Equals, "topic")
}

func (sa *snapadderSuite) TestDownPublishNoPID(c *C) {
	deviceID, err := utils.GetDeviceId()
	c.Assert(err, IsNil)

	translatedTopic := fmt.Sprintf("/%s/canonical/topic", deviceID)
	err = sa.handler.DownPublish(sa.ctx, &translatedTopic, &sa.userProps)
	c.Assert(err, IsNil)
}

func (sa *snapadderSuite) TestDownPublishLocalNamespace(c *C) {
	translatedTopic := "topic"
	err := sa.handler.DownPublish(sa.ctx, &translatedTopic, &sa.userProps)
	c.Assert(err, IsNil)

	c.Check(translatedTopic, Equals, "topic")
}

func (sa *snapadderSuite) TestConnect(c *C) {
	err := sa.handler.Connect(sa.ctx)

	c.Assert(err, IsNil)
}

func (sa *snapadderSuite) TestPublish(c *C) {
	basePayload := []byte("Hello World")
	err := sa.handler.Publish(sa.ctx, &sa.topic, &basePayload)
	c.Assert(err, IsNil)

	payloadAsString := string(basePayload)
	c.Check(payloadAsString, Equals, "Hello World")
}

func (sa *snapadderSuite) TestSubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: sa.topic}}
	err := sa.handler.Subscribe(sa.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(sa.topic, Equals, sampleTopic)
}

func (sa *snapadderSuite) TestUnsubscribe(c *C) {
	subs := []packets.SubOptions{{Topic: sa.topic}}
	err := sa.handler.Unsubscribe(sa.ctx, &subs)
	c.Assert(err, IsNil)

	c.Check(sa.topic, Equals, sampleTopic)
}

func (sa *snapadderSuite) TestDisconnect(c *C) {
	err := sa.handler.Disconnect(sa.ctx)

	c.Assert(err, IsNil)
}

func (sa *snapadderSuite) TestNoSession(c *C) {
	err := sa.handler.AuthConnect(context.Background(), false, nil)
	c.Check(err, NotNil)
}

func (sa *snapadderSuite) TestAuthConnectWithCertificate(c *C) {
	fakeCertificate := x509.Certificate{Subject: pkix.Name{CommonName: "fake_cert"}}
	ctxWithCertificate := session.NewContext(context.Background(), &session.Session{Cert: fakeCertificate})

	err := sa.handler.AuthConnect(ctxWithCertificate, false, nil)
	c.Check(err, IsNil)
}

func (sa *snapadderSuite) TestTopicIsAllowedIncorrectGlobal(c *C) {
	_, err := snapadder.MockIsAllowedTopic(sa.snapClient, "/my-topic", "", "", "")

	c.Check(err, NotNil)
}

func (sa *snapadderSuite) TestTopicIsAllowedSamePublisher(c *C) {
	result, err := snapadder.MockIsAllowedTopic(sa.snapClient, "/device-1/canonical/my-topic/hello", "", "canonical", "")

	c.Check(err, IsNil)
	c.Check(result, Equals, true)
}

func (sa *snapadderSuite) TestTopicIsAllowedConfdbView(c *C) {
	url := mockConfdbServer(c)

	confdbClient := client.New(&client.Config{BaseURL: url})
	result1, err1 := snapadder.MockIsAllowedTopic(confdbClient, "/device-1/canonical/my-topic/hello", "mqttx", "emqx", "sub")
	result2, err2 := snapadder.MockIsAllowedTopic(confdbClient, "/device-1/canonical/my-topic/hello", "mqttx", "emqx", "pub")
	result3, err3 := snapadder.MockIsAllowedTopic(confdbClient, "/device-2/canonical/my-topic/hello", "mqttx", "emqx", "sub")

	c.Check(err1, IsNil)
	c.Check(err2, IsNil)
	c.Check(err3, IsNil)
	c.Check(result1, Equals, true)
	c.Check(result2, Equals, false)
	c.Check(result3, Equals, false)
}

func (sa *snapadderSuite) TestTopicIsAllowedNoWildcard(c *C) {
	result1 := snapadder.MockIsContainedIn("/my-topic/hello", "/my-topic/hello")
	result2 := snapadder.MockIsContainedIn("/my-topic/hello", "/my-topic/goodbye")
	result3 := snapadder.MockIsContainedIn("/my-topic/hello", "/my-topic")

	c.Check(result1, Equals, true)
	c.Check(result2, Equals, false)
	c.Check(result3, Equals, false)
}

func (sa *snapadderSuite) TestTopicIsAllowedPlusWildcard(c *C) {
	result1 := snapadder.MockIsContainedIn("/my-topic/+/hello", "/my-topic/omar/hello")
	result2 := snapadder.MockIsContainedIn("/my-topic/+/hello", "/my-topic/omar")
	result3 := snapadder.MockIsContainedIn("/my-topic/+/hello", "/my-topic/omar/hello/something")

	c.Check(result1, Equals, true)
	c.Check(result2, Equals, false)
	c.Check(result3, Equals, false)
}

func (sa *snapadderSuite) TestTopicIsAllowedHashWildcard(c *C) {
	result1 := snapadder.MockIsContainedIn("/my-topic/#", "/my-topic/omar/hello")

	c.Check(result1, Equals, true)
}

func (sa *snapadderSuite) TestTopicIsAllowedMultipleWildcard(c *C) {
	result1 := snapadder.MockIsContainedIn("+/my-topic/#", "device-1/my-topic/omar/hello")

	c.Check(result1, Equals, true)
}

func mockConfdbServer(c *C) string {
	fail := func(w http.ResponseWriter, err error) {
		w.WriteHeader(500)
		fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
		c.Error(err)
	}

	var reqs int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0, 1, 2:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/confdb/6mluykWFsbpSV8RGPGjH7KFAkAOvdRyN/telem-agent/control-topics")

			q := r.URL.Query()
			fields := strutil.CommaSeparatedList(q.Get("fields"))
			c.Check(fields, DeepEquals, []string{"mqttx.sub"})

			w.WriteHeader(200)
			fmt.Fprintf(w, syncResp, `{"mqttx.sub": ["/device-1/canonical/my-topic/hello"]}`)
		default:
			err := fmt.Errorf("expected to get 3 requests, now on %d (%v)", reqs+1, r)
			fail(w, err)
		}

		reqs++
	}))

	return server.URL
}
