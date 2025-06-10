// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"crypto/tls"

	"github.com/caarlos0/env/v11"
	mptls "github.com/snapcore/snapd/telemagent/pkg/tls"
)

type Config struct {
	Address    string `env:"ADDRESS"     envDefault:""`
	PathPrefix string `env:"PATH_PREFIX" envDefault:"/"`
	Target     string `env:"TARGET"      envDefault:""`
	TLSConfig  *tls.Config
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

	c.TLSConfig, err = mptls.Load(&cfg)
	if err != nil {
		return Config{}, err
	}
	return c, nil
}
