// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package mqtt

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/canonical/telem-agent/config"
	"github.com/canonical/telem-agent/pkg/session"
	mptls "github.com/canonical/telem-agent/pkg/tls"
	"golang.org/x/sync/errgroup"
)

// Proxy is main MQTT proxy struct.
type Proxy struct {
	config      config.Config
	handler     session.Handler
	interceptor session.Interceptor
	logger      *slog.Logger
	dialer      tls.Dialer
}

// New returns a new MQTT Proxy instance.
func New(config config.Config, handler session.Handler, interceptor session.Interceptor, logger *slog.Logger) *Proxy {
	return &Proxy{
		config:      config,
		handler:     handler,
		logger:      logger,
		interceptor: interceptor,
	}
}

func (p Proxy) accept(ctx context.Context, l net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := l.Accept()
			if err != nil {
				p.logger.Warn("Accept error " + err.Error())
				continue
			}
			p.logger.Info("Accepted new client")
			go p.handle(ctx, conn)
		}
	}
}

func (p Proxy) handle(ctx context.Context, inbound net.Conn) {
	defer p.close(inbound)
	outbound, err := p.dialer.Dial("tcp", p.config.Target)
	if err != nil {
		p.logger.Error("Cannot connect to remote broker " + p.config.Target + " due to: " + err.Error())
		return
	}
	defer p.close(outbound)

	if err = session.Stream(ctx, inbound, outbound, p.handler, p.interceptor); err != io.EOF {
		p.logger.Warn(err.Error())
	}
}

// Listen of the server, this will block.
func (p Proxy) Listen(ctx context.Context) error {
	l, err := net.Listen("tcp", p.config.Address)
	if err != nil {
		return err
	}

	if p.config == (config.Config{}) {
		return fmt.Errorf("empty configuration, cannot listen")
	}

	if p.config.TLSConfig != nil {
		l = tls.NewListener(l, p.config.TLSConfig)
	}
	status := mptls.SecurityStatus(p.config.TLSConfig)
	p.logger.Info(fmt.Sprintf("telem-agent started at %s  with %s", p.config.Address, status))
	g, ctx := errgroup.WithContext(ctx)

	// Acceptor loop
	g.Go(func() error {
		p.accept(ctx, l)
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		return l.Close()
	})
	if err := g.Wait(); err != nil {
		p.logger.Info(fmt.Sprintf("telem-agent at %s with %s exiting with errors", p.config.Address, status), slog.String("error", err.Error()))
	} else {
		p.logger.Info(fmt.Sprintf("telem-agent at %s with %s exiting...", p.config.Address, status))
	}
	return nil
}

func (p Proxy) close(conn net.Conn) {
	if err := conn.Close(); err != nil {
		p.logger.Warn(fmt.Sprintf("Error closing connection %s", err.Error()))
	}
}
