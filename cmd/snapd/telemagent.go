package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/telemagent/pkg/hooks"
	mptls "github.com/snapcore/snapd/telemagent/pkg/tls"
	"github.com/snapcore/snapd/telemagent/pkg/utils"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
)

const mqttPrefix = "MQTT_"

func telemagent() {

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
	serverConfig, err := buildConfig()
	if err != nil {
		panic(err)
	}

	// Create logger with custom handler
	logger := slog.New(logHandler)

	// addEnv(logger)
	if serverConfig.Email == "" {
		brandAccount, err := utils.GetBrandAccount()
		if err != nil {
			panic(err)
		}

		serverConfig.Email = brandAccount
	}

	// Create the new MQTT Server.
	server := mochi.New(&mochi.Options{
		Logger:       logger,
		InlineClient: true,
	})

	// Allow all connections.
	err = server.AddHook(new(hooks.TelemAgentHook), &hooks.TelemAgentHookOptions{
		Server: server,
		Cfg:    *serverConfig,
	})

	if err != nil {
		log.Fatal(err)
	}

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP(listeners.Config{
		ID:        "t1",
		Address:   serverConfig.BrokerPort,
		TLSConfig: serverConfig.TLSConfig,
	})
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

func addEnv(logger *slog.Logger) {

	snapClient := client.New(nil)

	if os.Getenv("MQTT_ENDPOINT") == "" {
		logger.Info("config was empty")
		os.Setenv("MQTT_ENDPOINT", "mqtt://demo.staging:1883")

		_, err := snapClient.SetConf("system", map[string]any{"telemagent.endpoint": "mqtt://demo.staging:1883"})
		if err != nil {
			logger.Error(err.Error())
		}
	}

	if os.Getenv("MQTT_BROKER_PORT") == "" {
		os.Setenv("MQTT_BROKER_PORT", ":1885")

		_, err := snapClient.SetConf("system", map[string]any{"telemagent.port": ":1885"})
		if err != nil {
			logger.Error(err.Error())
		}
	}

	if os.Getenv("MQTT_SERVER_CA_FILE") == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			logger.Error(err.Error())
		}
		sslDir := filepath.Join(home, ".ssl")

		certFile, serverCAFile, keyFile, err := generateCertificates(sslDir)
		if err != nil {
			logger.Error(err.Error())
		}
		os.Setenv("MQTT_CERT_FILE", certFile)
		os.Setenv("MQTT_KEY_FILE", keyFile)
		os.Setenv("MQTT_SERVER_CA_FILE", serverCAFile)

		_, err = snapClient.SetConf("system", map[string]any{"telemagent.ca-cert": serverCAFile})
		if err != nil {
			logger.Error(err.Error())
		}

	}

	logger.Info("loaded env vars")
}

func generateCertificates(outDir string) (string, string, string, error) {
	// Create output directory if missing
	err := os.MkdirAll(outDir, 0700) // user-only access
	if err != nil {
		return "", "", "", err
	}

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", err
	}
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			Organization: []string{"Local CA"},
			CommonName:   "Local MQTT Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	caCertBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return "", "", "", err
	}
	var caCertPEM bytes.Buffer
	pem.Encode(&caCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: caCertBytes})

	// Server Keypair and signing
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", err
	}
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2048),
		Subject: pkix.Name{
			Organization: []string{"telem-agent"},
			CommonName:   "localhost",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"},
	}
	serverCertBytes, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return "", "", "", err
	}
	var serverCertPEM bytes.Buffer
	pem.Encode(&serverCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: serverCertBytes})

	var serverKeyPEM bytes.Buffer
	pem.Encode(&serverKeyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	// Write to files
	caPath := filepath.Join(outDir, "ca.crt")
	certPath := filepath.Join(outDir, "cert.crt")
	keyPath := filepath.Join(outDir, "key.crt")

	err = os.WriteFile(caPath, caCertPEM.Bytes(), 0644)
	if err != nil {
		return "", "", "", err
	}
	err = os.WriteFile(certPath, serverCertPEM.Bytes(), 0644)
	if err != nil {
		return "", "", "", err
	}
	err = os.WriteFile(keyPath, serverKeyPEM.Bytes(), 0600) // key should be protected
	if err != nil {
		return "", "", "", err
	}

	return caPath, certPath, keyPath, nil
}

func buildConfig() (*hooks.Config, error) {

	var cfg hooks.Config

	endpoint, err := setIfEmpty("telemagent.endpoint", "mqtt://demo.staging:1883")
	if err != nil {
		return nil, err
	}

	cfg.Endpoint = endpoint

	port, err := setIfEmpty("telemagent.port", ":1885")
	if err != nil {
		return nil, err
	}

	cfg.BrokerPort = port

	_, err = setIfEmpty("telemagent.telemgw-url", "http://demo.staging/stg-telemetry-k8s-telemgw")
	if err != nil {
		return nil, err
	}

	email, err := setIfEmpty("telemagent.email", "generic")
	if err != nil {
		return nil, err
	}

	cfg.Email = email

	certFile, serverCAFile, keyFile, err := generateCertificates("/etc/ssl")
	if err != nil {
		return nil, err
	}

	serverCAFile, err = setIfEmpty("telemagent.ca-cert", serverCAFile)
	if err != nil {
		return nil, err
	}

	logger.Debugf("server ca file: %s", serverCAFile)
	logger.Debugf("cert ca file: %s", certFile)
	logger.Debugf("key file: %s", keyFile)

	tlsCfg := mptls.Config{
		CertFile:     serverCAFile,
		ServerCAFile: serverCAFile,
		KeyFile:      keyFile,
	}

	cfg.TLSConfig, err = mptls.Load(&tlsCfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func defaultConfig() *hooks.Config {
	return &hooks.Config{
		Enabled:    true,
		Endpoint:   "mqtt://demo.staging:1883",
		BrokerPort: ":1885",
	}
}

func setIfEmpty(conf, value string) (string, error) {
	snapClient := client.New(nil)

	confs, err := snapClient.Conf("system", []string{conf})
	if err != nil && err.(*client.Error).Kind != client.ErrorKindConfigNoSuchOption {
		return value, err
	}

	if len(confs) > 1 {
		return value, errors.New("multiple configs found")
	}

	var confStr string
	var ok bool
	if len(confs) == 1 {
		confStr, ok = confs[conf].(string)
		if !ok {
			return value, errors.New("cannot convert to string")
		}
	}

	if confStr == "" {
		_, err := snapClient.SetConf("system", map[string]any{conf: value})
		if err != nil {
			return value, err
		}
	} else {
		return confStr, nil
	}

	return value, nil
}
