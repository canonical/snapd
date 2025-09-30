package main

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/snapcore/snapd/telemagent/pkg/hooks"

	"github.com/caarlos0/env/v11"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
)

const mqttPrefix = "MQTT_"

func telemagent() {
	addEnv()

	// Create signals channel to run server until interrupted
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	// Create Logger
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Remove time attribute
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	})

	// rest HTTP server Configuration
	serverConfig, err := hooks.NewConfig(env.Options{Prefix: mqttPrefix})
	if err != nil {
		panic(err)
	}

	// Create logger with custom handler
	logger := slog.New(logHandler)

	// Create the new MQTT Server.
	server := mochi.New(&mochi.Options{
		Logger:       logger,
		InlineClient: true,
	})

	// Allow all connections.
	err = server.AddHook(new(hooks.TelemAgentHook), &hooks.TelemAgentHookOptions{
		Server: server,
		Cfg:    serverConfig,
	})

	if err != nil {
		log.Fatal(err)
	}

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP(listeners.Config{ID: "t1", Address: serverConfig.BrokerPort})
	err = server.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			log.Fatal(err)
		}
	}()

	// Run server until interrupted
	<-done

	// Cleanup
}

func addEnv() {
	os.Setenv("MQTT_ENDPOINT", "mqtt://demo.staging:1883")
	os.Setenv("MQTT_BROKER_PORT", ":1885")
}
