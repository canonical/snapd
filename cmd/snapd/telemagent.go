// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/caarlos0/env/v11"
	"github.com/snapcore/snapd/telemagent/config"
	"github.com/snapcore/snapd/telemagent/handlers/simple"
	"github.com/snapcore/snapd/telemagent/handlers/snapadder"
	"github.com/snapcore/snapd/telemagent/handlers/userinjector"
	"github.com/snapcore/snapd/telemagent/interceptors/permissioncontroller"
	"github.com/snapcore/snapd/telemagent/pkg/mqtt"
	"github.com/snapcore/snapd/telemagent/pkg/rest"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	"golang.org/x/sync/errgroup"
)

const (
	mqttWithoutTLS = "MPROXY_MQTT_WITHOUT_TLS_"
	mqttWithTLS    = "MPROXY_MQTT_WITH_TLS_"
	mqttWithmTLS   = "MPROXY_MQTT_WITH_MTLS_"
	restPrefix     = "REST_"
)

func telemagent() {
	addEnv()

	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	// Create a handler that removes timestamps
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Remove time attribute
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	})

	// Create logger with custom handler
	logger := slog.New(logHandler)
	handlerType := flag.Int("hand", 4, "selects the handler that inspects the packets")

	interceptor := permissioncontroller.New(logger)

	flag.Parse()

	var handler session.Handler

	switch *handlerType {
	case 0:
		handler = simple.New(logger)
	case 1:
		handler = userinjector.New(logger)
	case 4:
		handler = snapadder.New(logger)
	default:
		handler = snapadder.New(logger)
	}


	// mProxy server Configuration for MQTT without TLS
	mqttConfig, err := config.NewConfig(env.Options{Prefix: mqttWithoutTLS})
	if err != nil {
		panic(err)
	}

	// mProxy server for MQTT without TLS
	mqttProxy := mqtt.New(mqttConfig, handler, interceptor, logger)
	g.Go(func() error {
		return mqttProxy.Listen(ctx)
	})

	// mProxy server Configuration for MQTT with TLS
	mqttTLSConfig, err := config.NewConfig(env.Options{Prefix: mqttWithTLS})
	if err != nil {
		panic(err)
	}

	// mProxy server for MQTT with TLS
	mqttTLSProxy := mqtt.New(mqttTLSConfig, handler, interceptor, logger)
	g.Go(func() error {
		return mqttTLSProxy.Listen(ctx)
	})

	// rest HTTP server Configuration
	restConfig, err := rest.NewConfig(env.Options{Prefix: restPrefix})
	if err != nil {
		panic(err)
	}
	if restConfig.Enabled {
		restServer, err := rest.NewServer(restConfig, logger, nil)
		if err != nil {
			panic(err)
		}
		g.Go(func() error {
			return restServer.Start(ctx)
		})
	}

	g.Go(func() error {
		return StopSignalHandler(ctx, cancel, logger)
	})

	if err := g.Wait(); err != nil {
		logger.Error(fmt.Sprintf("telem-agent service terminated with error: %s", err))
	} else {
		logger.Info("telem-agent service stopped")
	}
}

func StopSignalHandler(ctx context.Context, cancel context.CancelFunc, logger *slog.Logger) error {
	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGINT, syscall.SIGABRT)
	select {
	case <-c:
		cancel()
		return nil
	case <-ctx.Done():
		return nil
	}
}

func addEnv() {
	os.Setenv("MPROXY_MQTT_WITHOUT_TLS_ADDRESS",":1884")
	os.Setenv("MPROXY_MQTT_WITHOUT_TLS_TARGET","broker.emqx.io:1883")

	os.Setenv("MPROXY_MQTT_WITH_TLS_ADDRESS",":8883")
	os.Setenv("MPROXY_MQTT_WITH_TLS_TARGET","broker.emqx.io:8883")
	os.Setenv("MPROXY_MQTT_WITH_TLS_CERT_FILE","/home/omar/telem-agent/ssl/certs/server.crt")
	os.Setenv("MPROXY_MQTT_WITH_TLS_KEY_FILE","/home/omar/telem-agent/ssl/certs/server.key")
	os.Setenv("MPROXY_MQTT_WITH_TLS_SERVER_CA_FILE","/home/omar/telem-agent/ssl/certs/ca.crt")


	os.Setenv("REST_ENABLED","true")
	os.Setenv("REST_ENDPOINT","mqtts://broker.emqx.io:8883")
	os.Setenv("REST_SERVER_CA_FILE","/home/omar/telem-agent/ssl/certs/broker.emqx.io-ca.crt")

}