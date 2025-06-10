// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"github.com/caarlos0/env/v11"
)

type Config struct {
	CertFile     string `env:"CERT_FILE"      envDefault:""`
	KeyFile      string `env:"KEY_FILE"       envDefault:""`
	ServerCAFile string `env:"SERVER_CA_FILE" envDefault:""`
	ClientCAFile string `env:"CLIENT_CA_FILE" envDefault:""`
}

func NewConfig(opts env.Options) (Config, error) {
	c := Config{}
	var err error
	if err = env.ParseWithOptions(&c, opts); err != nil {
		return Config{}, err
	}

	return c, nil
}
