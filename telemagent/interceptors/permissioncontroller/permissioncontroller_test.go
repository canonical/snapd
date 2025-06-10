// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package permissioncontroller_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/snapcore/snapd/telemagent/interceptors/permissioncontroller"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type permissioncontrollerSuite struct {
	ctx            context.Context
	permController permissioncontroller.PermissionController
}

var _ = Suite(&permissioncontrollerSuite{})

func (s *permissioncontrollerSuite) SetUpSuite(c *C) {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	s.permController = *permissioncontroller.New(slog.New(logHandler))

	s.ctx = session.NewContext(context.Background(), &session.Session{})
}

func (ss *permissioncontrollerSuite) TestDownDir(c *C) {
	pkt := packets.NewControlPacket(packets.CONNECT)
	_, err := ss.permController.Intercept(ss.ctx, pkt, session.Down)
	c.Check(err, IsNil)
}

func (ss *permissioncontrollerSuite) TestUpDirNotSuborPuborConn(c *C) {
	pkt := packets.NewControlPacket(packets.UNSUBSCRIBE)
	_, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)
	c.Check(err, IsNil)
}

func (ss *permissioncontrollerSuite) TestUpDirConn(c *C) {
	pkt := packets.NewControlPacket(packets.CONNECT)
	_, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)
	c.Check(err, IsNil)
}

func (ss *permissioncontrollerSuite) TestUpDirBadPubQos0(c *C) {
	pkt := packets.NewControlPacket(packets.PUBLISH)
	pkt.Content.(*packets.Publish).Topic = permissioncontroller.DeniedTopic
	_, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)
	c.Check(err, Equals, session.ErrDeniedTopic)
}

func (ss *permissioncontrollerSuite) TestUpDirBadPubQos1(c *C) {
	pkt := packets.NewControlPacket(packets.PUBLISH)
	pkt.Content.(*packets.Publish).QoS = 1
	pkt.Content.(*packets.Publish).Topic = permissioncontroller.DeniedTopic
	deniedPacket, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)

	c.Check(err, Equals, session.ErrDeniedTopic)
	c.Check(deniedPacket.PacketType(), Equals, "PUBACK")
	c.Check(deniedPacket.Content.(*packets.Puback).PacketID, Equals, pkt.Content.(*packets.Publish).PacketID)
	c.Check(deniedPacket.Content.(*packets.Puback).ReasonCode, Equals, byte(packets.PubackNotAuthorized))
}

func (ss *permissioncontrollerSuite) TestUpDirBadPubQos2(c *C) {
	pkt := packets.NewControlPacket(packets.PUBLISH)
	pkt.Content.(*packets.Publish).QoS = 2
	pkt.Content.(*packets.Publish).Topic = permissioncontroller.DeniedTopic
	deniedPacket, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)

	c.Check(err, Equals, session.ErrDeniedTopic)
	c.Check(deniedPacket.PacketType(), Equals, "PUBREC")
	c.Check(deniedPacket.Content.(*packets.Pubrec).PacketID, Equals, pkt.Content.(*packets.Publish).PacketID)
	c.Check(deniedPacket.Content.(*packets.Pubrec).ReasonCode, Equals, byte(packets.PubrecNotAuthorized))
}

func (ss *permissioncontrollerSuite) TestUpDirGoodPub(c *C) {
	pkt := packets.NewControlPacket(packets.PUBLISH)
	pkt.Content.(*packets.Publish).Topic = "good/topic"

	_, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)
	c.Check(err, IsNil)
}

func (ss *permissioncontrollerSuite) TestUpDirBadSub(c *C) {
	pkt := packets.NewControlPacket(packets.SUBSCRIBE)

	pkt.Content.(*packets.Subscribe).Subscriptions = append(pkt.Content.(*packets.Subscribe).Subscriptions, packets.SubOptions{Topic: permissioncontroller.DeniedTopic})
	_, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)
	c.Check(err, Equals, session.ErrDeniedTopic)
}

func (ss *permissioncontrollerSuite) TestUpDirGoodSub(c *C) {
	pkt := packets.NewControlPacket(packets.SUBSCRIBE)

	pkt.Content.(*packets.Subscribe).Subscriptions = append(pkt.Content.(*packets.Subscribe).Subscriptions, packets.SubOptions{Topic: "good/topic"})
	_, err := ss.permController.Intercept(ss.ctx, pkt, session.Up)
	c.Check(err, IsNil)
}
