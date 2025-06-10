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
	"github.com/canonical/telem-agent/config"
	"github.com/canonical/telem-agent/handlers/simple"
	"github.com/canonical/telem-agent/handlers/snapadder"
	"github.com/canonical/telem-agent/handlers/userinjector"
	"github.com/canonical/telem-agent/interceptors/permissioncontroller"
	"github.com/canonical/telem-agent/internal/utils"
	"github.com/canonical/telem-agent/pkg/mqtt"
	"github.com/canonical/telem-agent/pkg/rest"
	"github.com/canonical/telem-agent/pkg/session"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
)

const (
	mqttWithoutTLS = "MPROXY_MQTT_WITHOUT_TLS_"
	mqttWithTLS    = "MPROXY_MQTT_WITH_TLS_"
	mqttWithmTLS   = "MPROXY_MQTT_WITH_MTLS_"
	restPrefix     = "REST_"
)

func main() {
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

	pathPtr := flag.String("env", "", "The .env path")
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

	if *pathPtr == "" {
		// Loading .env file to environment
		err := godotenv.Load()
		if err != nil {
			panic(err)
		}
	} else {
		// Loading specified file to environment
		err := godotenv.Load(*pathPtr)
		if err != nil {
			panic(err)
		}
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
		restServer, err := rest.NewServer(restConfig, logger, utils.GetSnapInfoFromConn, nil)
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
