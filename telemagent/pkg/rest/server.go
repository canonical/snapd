// Copyright (c) 2025 Canonical Ltd
// SPDX-License-Identifier: Apache-2.0
package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/canonical/mqtt.golang/autopaho"
	"github.com/canonical/mqtt.golang/autopaho/extensions/rpc"
	"github.com/canonical/mqtt.golang/paho"
	"github.com/snapcore/snapd/telemagent/internal/utils"
	mptls "github.com/snapcore/snapd/telemagent/pkg/tls"
	"golang.org/x/sync/errgroup"
)

type Config struct {
	Enabled   bool   `env:"ENABLED"     envDefault:"false"`
	Endpoint  string `env:"ENDPOINT"     envDefault:"mqtt://localhost:1883"`
	Port      int    `env:"PORT"     envDefault:"9090"`
	TLSConfig *tls.Config
}

func NewConfig(opts env.Options) (Config, error) {
	c := Config{}
	if err := env.ParseWithOptions(&c, opts); err != nil {
		return Config{}, err
	}

	cfg, err := mptls.NewConfig(opts)
	if err != nil {
		return Config{}, err
	}

	c.TLSConfig, err = mptls.LoadClient(&cfg)
	if err != nil {
		return Config{}, err
	}

	return c, nil
}

type Server struct {
	mqttConfig autopaho.ClientConfig
	mqttClient *autopaho.ConnectionManager
	ctx        context.Context
	router     paho.Router

	detectSnap func(string) (string, string, error)

	config Config
	logger *slog.Logger

	mux *http.ServeMux
}

func NewServer(cfg Config, logger *slog.Logger, brokerConn *net.Conn) (*Server, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	var attemptFunc func(ctx context.Context, cc autopaho.ClientConfig, u *url.URL) (net.Conn, error)
	if brokerConn != nil {
		attemptFunc = func(ctx context.Context, cc autopaho.ClientConfig, u *url.URL) (net.Conn, error) {return *brokerConn, nil}
	}


	router := paho.NewStandardRouter()

	deviceID, err := utils.GetDeviceId()
	if err != nil {
		return nil, err
	}

	cliCfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     20,
		CleanStartOnInitialConnection: false,
		SessionExpiryInterval:         60,
		TlsCfg:                        cfg.TLSConfig,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			logger.Info("Server connected to MQTT broker on address ")
		},
		OnConnectError: func(err error) { logger.Error(fmt.Sprintf("error whilst attempting connection: %s\n", err)) },
		AttemptConnection: attemptFunc,
		ClientConfig: paho.ClientConfig{
			ClientID:      deviceID + "-" + strconv.Itoa(1e4+rand.Int()%9e4),
			OnClientError: func(err error) { logger.Error(fmt.Sprintf("client error: %s\n", err)) },
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(p paho.PublishReceived) (bool, error) {
					router.Route(p.Packet.Packet())
					return false, nil
				},
			},
			OnServerDisconnect: func(d *paho.Disconnect) {
				if d.Properties != nil {
					logger.Info(fmt.Sprintf("server requested disconnect: %s\n", d.Properties.ReasonString))
				} else {
					logger.Info(fmt.Sprintf("server requested disconnect; reason code: %d\n", d.ReasonCode))
				}
			},
		},
	}

	mux := http.NewServeMux()

	return &Server{config: cfg, mqttConfig: cliCfg, mux: mux, logger: logger, router: router, detectSnap: utils.GetSnapInfoFromConn}, nil
}

func (s *Server) echoHandler(writer http.ResponseWriter, request *http.Request) {
	var buf bytes.Buffer
	err := request.Write(&buf)
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		writer.Write([]byte("Could not convert requests to byte stream"))
		s.logger.Error("Could not convert requests to byte stream")
		return
	}

	snapPublisher, snapName, err := s.detectSnap(request.RemoteAddr)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Could not get snap name: %s", err.Error()))
		writer.WriteHeader(http.StatusBadRequest)
		writer.Write(fmt.Appendf(nil, "Could not get snap name: %s", err.Error()))
		return
	} else {
		s.logger.Info(fmt.Sprintf("Obtained publisher info: %s, %s", snapPublisher, snapName))
	}

	s.logger.Info("Handling request made to " + request.URL.Path + " to client (" + request.RemoteAddr + ")")

	data := buf.Bytes()

	deviceId, err := utils.GetDeviceId()
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		writer.Write([]byte("Could not get device Id"))

		s.logger.Error(err.Error())
		return
	}

	levels := strings.Split(request.URL.Path, "/")[1:]
	if len(levels) == 0 {
		writer.WriteHeader(http.StatusBadRequest)
		writer.Write([]byte("no service name provided"))

		s.logger.Error("no service name provided")
		return
	}
	serviceName := levels[0]
	localSubTopic := fmt.Sprintf("/%s/%s/%s", deviceId, snapPublisher, serviceName)
	localPubTopic := localSubTopic + "/request"

	h, err := rpc.NewHandler(s.ctx, rpc.HandlerOpts{
		Conn:             s.mqttClient,
		Router:           s.router,
		ResponseTopicFmt: "%s/response",
		ClientID:         localSubTopic,
	})
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		writer.Write(fmt.Appendf(nil, "handler could not be created: %s", err))
		s.logger.Error(err.Error())
		return
	}
	s.logger.Info("Received response for request")

	resp, err := h.Request(s.ctx, &paho.Publish{
		Topic:   localPubTopic,
		Payload: data,
		QoS:     2,
	})
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		writer.Write(fmt.Appendf(nil, "error in response: %s", err))
		s.logger.Error(err.Error())
		return
	}

	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Content-Type", request.Header.Get("Content-Type"))
	writer.Write(resp.Payload)
}

func (s *Server) Start(ctx context.Context) error {
	c, err := autopaho.NewConnection(ctx, s.mqttConfig) // starts process; will reconnect until context cancelled
	if err != nil {
		return err
	}

	s.mqttClient = c
	s.ctx = ctx
	// Wait for the connection to come up
	if err = s.mqttClient.AwaitConnection(ctx); err != nil {
		return err
	}

	s.mux.HandleFunc("/", s.echoHandler)
	s.logger.Info(fmt.Sprintf("OTEL Server started at :%d", s.config.Port))

	g, ctx := errgroup.WithContext(ctx)

	httpServer := &http.Server{Addr: fmt.Sprintf(":%d", s.config.Port), Handler: s.mux}

	// Acceptor loop
	g.Go(func() error {
		return httpServer.ListenAndServe()
	})

	g.Go(func() error {
		<-ctx.Done()
		<-s.mqttClient.Done() // Wait for clean shutdown (cancelling the context triggered the shutdown)
		return httpServer.Shutdown(ctx)
	})

	if err := g.Wait(); err != nil {
		s.logger.Info(fmt.Sprintf("otel server at %d exiting with errors", s.config.Port), slog.String("error", err.Error()))
	} else {
		s.logger.Info(fmt.Sprintf("otel server at %d exiting...", s.config.Port))
	}

	return nil
}
