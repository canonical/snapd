// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/canonical/telem-agent/internal/utils"
	"golang.org/x/sync/errgroup"
)

type Direction int

const (
	Up Direction = iota
	Down
)

const (
	unknownID = "unknown"
)

var (
	errBroker      = "failed to proxy from MQTT client with id %s to MQTT broker with error: %s"
	errClient      = "failed to proxy from MQTT broker to client with id %s with error: %s"
	ErrDeniedTopic = fmt.Errorf("topic is not allowed")
)

// Stream starts proxy between client and broker.
func Stream(ctx context.Context, in, out net.Conn, h Handler, ic Interceptor) error {
	s := Session{}
	ctx = NewContext(ctx, &s)

	g, ctx := errgroup.WithContext(ctx)

	var protocolVersion byte

	err := stream(&ctx, Up, in, out, h, ic, &protocolVersion)
	if err != nil {
		return err
	}

	g.Go(func() error {
		return stream(&ctx, Up, in, out, h, ic, &protocolVersion)
	})

	g.Go(func() error {
		return stream(&ctx, Down, out, in, h, ic, &protocolVersion)
	})

	err = g.Wait()

	disconnectErr := h.Disconnect(ctx)

	return errors.Join(err, disconnectErr)
}

func stream(ctx *context.Context, dir Direction, r, w net.Conn, h Handler, ic Interceptor, protocolVersion *byte) error {
	for {
		select {
		case <-(*ctx).Done():
			return nil
		default:
		}

		var err error
		var pkt *packets.ControlPacket
		stop := false
		block := false

		if *protocolVersion == 0 {
			pkt, *protocolVersion, err = packets.GetProtocolVersion(r)
			stop = true
		} else {
			pkt, err = packets.ReadPacket(r, *protocolVersion)
		}

		if err != nil {
			return wrap(*ctx, err, dir)
		}

		if _, _, ok := GetSnapFromContext(*ctx); !ok {
			var snapPublisher, snapName string
			snapPublisher, snapName, err = utils.GetSnapInfoFromConn(r.RemoteAddr().String())
			*ctx = AddSnapToContext(*ctx, snapPublisher, snapName)
			stop = true
		}

		if err != nil {
			return wrap(*ctx, err, dir)
		}

		switch dir {
		case Up:
			if err = authorize(*ctx, pkt, h); err != nil {
				return wrap(*ctx, err, dir)
			}

		default:

			if pkt.Type == packets.PUBLISH {
				if err = h.DownPublish(*ctx, &pkt.Content.(*packets.Publish).Topic, &pkt.Content.(*packets.Publish).Properties.User); err != nil {
					pkt = packets.NewControlPacket(packets.DISCONNECT)
					if _, wErr := pkt.WriteTo(w); wErr != nil {
						err = errors.Join(err, wErr)
					}
					return wrap(*ctx, err, dir)
				}
			}
		}

		if ic != nil {
			*pkt, err = ic.Intercept(*ctx, pkt, dir)
			if err == ErrDeniedTopic {
				block = true
			} else if err != nil {
				return wrap(*ctx, err, dir)
			}
		}

		if !block {
			// Send to another.
			if _, err := pkt.WriteTo(w); err != nil {
				return wrap(*ctx, err, dir)
			}

			// Notify only for packets sent from client to broker (incoming packets).
			if dir == Up {
				if err := notify(*ctx, pkt, h); err != nil {
					return wrap(*ctx, err, dir)
				}
			}
		} else {
			if pkt.Content != nil {
				if _, err := pkt.WriteTo(r); err != nil {
					return wrap(*ctx, err, dir)
				}
			}
		}

		if stop {
			return nil
		}
	}
}

func authorize(ctx context.Context, pkt *packets.ControlPacket, h Handler) error {
	switch p := pkt.PacketType(); p {
	case "CONNECT":
		s, ok := FromContext(ctx)
		if ok {
			s.ID = pkt.Content.(*packets.Connect).ClientID
			s.Username = pkt.Content.(*packets.Connect).Username
			s.Password = pkt.Content.(*packets.Connect).Password
		}

		ctx = NewContext(ctx, s)
		if err := h.AuthConnect(ctx, pkt.Content.(*packets.Connect).WillFlag, &pkt.Content.(*packets.Connect).WillTopic); err != nil {
			return err
		}
		// Copy back to the packet in case values are changed by Event handler.
		// This is specific to CONN, as only that package type has credentials.
		pkt.Content.(*packets.Connect).ClientID = s.ID
		pkt.Content.(*packets.Connect).Username = s.Username
		pkt.Content.(*packets.Connect).Password = s.Password
		return nil
	case "PUBLISH":
		return h.AuthPublish(ctx, &pkt.Content.(*packets.Publish).Topic, &pkt.Content.(*packets.Publish).Payload, &pkt.Content.(*packets.Publish).Properties.User)
	case "SUBSCRIBE":
		return h.AuthSubscribe(ctx, &pkt.Content.(*packets.Subscribe).Subscriptions, &pkt.Content.(*packets.Subscribe).Properties.User)
	case "UNSUBSCRIBE":
		return h.AuthUnsubscribe(ctx, &pkt.Content.(*packets.Unsubscribe).Topics, &pkt.Content.(*packets.Unsubscribe).Properties.User)
	default:
		return nil
	}
}

func notify(ctx context.Context, pkt *packets.ControlPacket, h Handler) error {
	switch p := pkt.PacketType(); p {
	case "CONNECT":
		return h.Connect(ctx)
	case "PUBLISH":
		return h.Publish(ctx, &pkt.Content.(*packets.Publish).Topic, &pkt.Content.(*packets.Publish).Payload)
	case "SUBSCRIBE":
		return h.Subscribe(ctx, &pkt.Content.(*packets.Subscribe).Subscriptions)
	case "UNSUBSCRIBE	":
		return h.Unsubscribe(ctx, &pkt.Content.(*packets.Subscribe).Subscriptions)
	default:
		return nil
	}
}

func wrap(ctx context.Context, err error, dir Direction) error {
	if err == io.EOF {
		return err
	}
	cid := unknownID
	if s, ok := FromContext(ctx); ok {
		cid = s.ID
	}
	switch dir {
	case Up:
		return fmt.Errorf(errClient, cid, err.Error())
	case Down:
		return fmt.Errorf(errBroker, cid, err.Error())
	default:
		return err
	}
}
