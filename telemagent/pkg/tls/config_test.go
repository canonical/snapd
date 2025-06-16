// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0
package tls_test

import (
	"github.com/caarlos0/env/v11"
	"github.com/snapcore/snapd/telemagent/pkg/tls"
	. "gopkg.in/check.v1"
)

const (
	mqttWithoutTLS = "MPROXY_MQTT_WITHOUT_TLS_"
	mqttWithTLS    = "MPROXY_MQTT_WITH_TLS_"
	mqttWithmTLS   = "MPROXY_MQTT_WITH_MTLS_"
)

type configSuite struct {
	CertFile     string `env:"CERT_FILE"      envDefault:""`
	KeyFile      string `env:"KEY_FILE"       envDefault:""`
	ServerCAFile string `env:"SERVER_CA_FILE" envDefault:""`
	ClientCAFile string `env:"CLIENT_CA_FILE" envDefault:""`
}

var _ = Suite(&configSuite{})

func (cs *configSuite) SetUpSuite(c *C) {
}

func (cs *configSuite) TestNewConfig(c *C) {
	envOpts := env.Options{Prefix: mqttWithoutTLS}
	_, err := tls.NewConfig(envOpts)

	c.Check(err, IsNil)
}
