// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0
package session_test

import (
	"context"
	"log/slog"
	"net"
	"os"

	"github.com/canonical/mqtt.golang/mock/basictestserver"
	"github.com/canonical/mqtt.golang/packets"
	"github.com/canonical/mqtt.golang/paho"
	"github.com/snapcore/snapd/telemagent/handlers/simple"
	"github.com/snapcore/snapd/telemagent/handlers/testhandler"
	"github.com/snapcore/snapd/telemagent/interceptors/permissioncontroller"
	"github.com/snapcore/snapd/telemagent/internal/utils"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	. "gopkg.in/check.v1"
)

type streamSuite struct {
	ctx         context.Context
	interceptor session.Interceptor
	handler     *simple.Handler
	badHandler  *testhandler.Handler
	testServer  basictestserver.TestServer
	in          net.Conn
	out         net.Conn
	client      *paho.Client
}

var _ = Suite(&streamSuite{})

func (ss *streamSuite) SetUpSuite(c *C) {
	ss.ctx = context.Background()

	ss.ctx = session.AddSnapToContext(ss.ctx, "canonical", "multipass")

	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(logHandler)
	ss.handler = simple.New(logger)
	ss.interceptor = permissioncontroller.New(logger)
	ss.badHandler = testhandler.New(logger)
}

func (ss *streamSuite) SetUpTest(c *C) {
	var serverLogger basictestserver.Logger
	ss.testServer = *basictestserver.New(serverLogger)

	var err error
	ss.in, ss.out, err = utils.NetPipe(ss.ctx)
	if err != nil {
		panic(err)
	}

	ss.client = paho.NewClient(paho.ClientConfig{
		Conn: ss.in,
	})
}

func (ss *streamSuite) TestStreamInvalidConn(c *C) {
	var in, out net.Conn
	err := session.Stream(ss.ctx, in, out, ss.handler, ss.interceptor)

	c.Check(err, NotNil)
}

func (ss *streamSuite) TestStreamConnectPacket(c *C) {
	ss.ctx = session.AddSnapToContext(ss.ctx, "canonical", "multipass")
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
	}

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.handler, ss.interceptor)
	ca, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, IsNil)
	c.Assert(ca.ReasonCode, Equals, uint8(0))

	ss.client.Disconnect(&paho.Disconnect{})
	defer ss.testServer.Stop()
}

func (ss *streamSuite) TestStreamAuthConnectFailed(c *C) {
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
	}

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.badHandler, ss.interceptor)
	_, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, NotNil)

	ss.client.Disconnect(&paho.Disconnect{})
	defer ss.testServer.Stop()
}

func (ss *streamSuite) TestStreamSubscribePacket(c *C) {
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	ss.testServer.SetResponse(packets.SUBACK, &packets.Suback{
		Reasons:    []byte{1, 2, 0},
		Properties: &packets.Properties{},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
		WillMessage: &paho.WillMessage{
			Topic:   "will/topic",
			Payload: []byte("am gone"),
		},
		WillProperties: &paho.WillProperties{
			WillDelayInterval: paho.Uint32(200),
		},
	}

	s := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: "test/1", QoS: 1},
			{Topic: "test/2", QoS: 2},
			{Topic: "test/3", QoS: 0},
		},
	}

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.handler, ss.interceptor)
	_, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, IsNil)
	// c.Assert(ca.ReasonCode, Equals, uint8(0))
	_, err = ss.client.Subscribe(context.Background(), s)
	c.Check(err, IsNil)

	ss.client.Disconnect(&paho.Disconnect{})

	defer ss.testServer.Stop()
}

func (ss *streamSuite) TestStreamSubscribePacketFail(c *C) {
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	ss.testServer.SetResponse(packets.SUBACK, &packets.Suback{
		Reasons:    []byte{1, 2, 0},
		Properties: &packets.Properties{},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
		WillMessage: &paho.WillMessage{
			Topic:   "will/topic",
			Payload: []byte("am gone"),
		},
		WillProperties: &paho.WillProperties{
			WillDelayInterval: paho.Uint32(200),
		},
	}

	s := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: "test/1", QoS: 1},
			{Topic: "test/2", QoS: 2},
			{Topic: "test/3", QoS: 0},
		},
	}

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.badHandler, ss.interceptor)
	_, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, NotNil)
	// c.Assert(ca.ReasonCode, Equals, uint8(0))
	_, err = ss.client.Subscribe(context.Background(), s)
	c.Check(err, NotNil)

	ss.client.Disconnect(&paho.Disconnect{})

	defer ss.testServer.Stop()
}

func (ss *streamSuite) TestStreamPublishPacket(c *C) {
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
		WillMessage: &paho.WillMessage{
			Topic:   "will/topic",
			Payload: []byte("am gone"),
		},
		WillProperties: &paho.WillProperties{
			WillDelayInterval: paho.Uint32(200),
		},
	}

	p := &paho.Publish{
		Topic:   "test/0",
		QoS:     0,
		Payload: []byte("test payload"),
	}

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.handler, ss.interceptor)
	_, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, IsNil)
	pr, err := ss.client.Publish(context.Background(), p)
	c.Check(err, IsNil)
	c.Check(pr, NotNil)

	ss.client.Disconnect(&paho.Disconnect{})

	defer ss.testServer.Stop()
}

func (ss *streamSuite) TestStreamPublishPacketDenied(c *C) {
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
		WillMessage: &paho.WillMessage{
			Topic:   "will/topic",
			Payload: []byte("am gone"),
		},
		WillProperties: &paho.WillProperties{
			WillDelayInterval: paho.Uint32(200),
		},
	}

	p := &paho.Publish{
		Topic:   permissioncontroller.DeniedTopic,
		QoS:     1,
		Payload: []byte("test payload"),
	}

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.handler, ss.interceptor)
	_, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, IsNil)
	pr, err := ss.client.Publish(context.Background(), p)
	c.Check(err, NotNil)
	c.Check(pr, NotNil)
	c.Check(pr.ReasonCode, Equals, uint8(packets.PubackNotAuthorized))

	ss.client.Disconnect(&paho.Disconnect{})

	defer ss.testServer.Stop()
}

func (ss *streamSuite) TestStreamUnsubscribePacket(c *C) {
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	ss.testServer.SetResponse(packets.UNSUBACK, &packets.Unsuback{
		Reasons:    []byte{0, 17},
		Properties: &packets.Properties{},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
		WillMessage: &paho.WillMessage{
			Topic:   "will/topic",
			Payload: []byte("am gone"),
		},
		WillProperties: &paho.WillProperties{
			WillDelayInterval: paho.Uint32(200),
		},
	}

	u := &paho.Unsubscribe{
		Topics: []string{
			"test/1",
			"test/2",
		},
	}

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.handler, ss.interceptor)
	_, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, IsNil)
	_, err = ss.client.Unsubscribe(context.Background(), u)
	c.Check(err, IsNil)

	ss.client.Disconnect(&paho.Disconnect{})

	defer ss.testServer.Stop()
}

func (ss *streamSuite) TestStreamDownDirection(c *C) {
	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	go ss.testServer.Run()
	go session.Stream(ss.ctx, ss.out, ss.testServer.ClientConn(), ss.handler, ss.interceptor)
	defer ss.testServer.Stop()

	rChan := make(chan struct{})
	ss.client.AddOnPublishReceived(func(pr paho.PublishReceived) (bool, error) {
		c.Assert(pr.Packet.Topic, Equals, "test/0")
		c.Assert(string(pr.Packet.Payload), Equals, "test payload")
		c.Assert(pr.Packet.QoS, Equals, byte(0))
		close(rChan)
		return true, nil
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
		WillMessage: &paho.WillMessage{
			Topic:   "will/topic",
			Payload: []byte("am gone"),
		},
		WillProperties: &paho.WillProperties{
			WillDelayInterval: paho.Uint32(200),
		},
	}

	_, err := ss.client.Connect(context.Background(), cp)
	c.Check(err, IsNil)

	err = ss.testServer.SendPacket(&packets.Publish{
		Topic:   "test/0",
		QoS:     0,
		Payload: []byte("test payload"),
	})

	c.Check(err, IsNil)
	<-rChan
	err = ss.client.Disconnect(&paho.Disconnect{})
	c.Check(err, IsNil)
}

func (ss *streamSuite) TestNestatBadConnection(c *C) {
	ctx := context.Background()
	in, out := net.Pipe()
	client := paho.NewClient(paho.ClientConfig{
		Conn: in,
	})

	ss.testServer.SetResponse(packets.CONNACK, &packets.Connack{
		ReasonCode:     0,
		SessionPresent: false,
		Properties: &packets.Properties{
			MaximumPacketSize: paho.Uint32(12345),
			MaximumQOS:        paho.Byte(1),
			ReceiveMaximum:    paho.Uint16(12345),
			TopicAliasMaximum: paho.Uint16(200),
		},
	})

	cp := &paho.Connect{
		KeepAlive:  30,
		ClientID:   "ss.testClient",
		CleanStart: true,
		Properties: &paho.ConnectProperties{
			ReceiveMaximum: paho.Uint16(200),
		},
	}

	go ss.testServer.Run()
	go session.Stream(ctx, out, ss.testServer.ClientConn(), ss.handler, ss.interceptor)
	_, err := client.Connect(context.Background(), cp)

	c.Check(err, NotNil)
}

func (ss *streamSuite) TestAddSnapInfoToContext(c *C) {
	ctx := context.Background()
	in, _, _ := utils.NetPipe(ctx)

	snapPublisher, snapName, err := utils.GetSnapInfoFromConn(in.RemoteAddr().String())
	session.AddSnapToContext(ctx, snapPublisher, snapName)

	c.Check(err, NotNil)
}
