package coreconfig

import (
	"io/ioutil"
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v2"
)

const (
	tzPathEnvironment string = "UBUNTU_CORE_CONFIG_TZ_FILE"
	tzPathDefault     string = "/etc/timezone"
)

type systemConfig struct {
	Timezone string `yaml:"timezone"`
}

type configYaml struct {
	Config *systemConfig
}

func newSystemConfig() (*systemConfig, error) {
	// TODO think of a smart way not to miss a config entry
	tz, err := getTimezone()
	if err != nil {
		return nil, err
	}

	config := &systemConfig{
		Timezone: tz,
	}

	return config, nil
}

// for testing purposes
var yamlMarshal = yaml.Marshal

// Get is a special configuration case for the system, for which
// there is no such entry in a package.yaml to satisfy the snappy config interface.
// This implements getting the current configuration for ubuntu-core.
func Get() (rawConfig string, err error) {
	config, err := newSystemConfig()
	if err != nil {
		return "", err
	}

	out, err := yamlMarshal(&configYaml{config})
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// Set is used to configure settings for the system, this is meant to
// be used as an interface for snappy config to satisfy the ubuntu-core
// hook.
func Set(rawConfig string) (newRawConfig string, err error) {
	oldConfig, err := newSystemConfig()
	if err != nil {
		return "", err
	}

	var configWrap configYaml
	err = yaml.Unmarshal([]byte(rawConfig), &configWrap)
	if err != nil {
		return "", err
	}
	newConfig := configWrap.Config

	rNewConfig := reflect.ValueOf(newConfig).Elem()
	rType := rNewConfig.Type()
	for i := 0; i < rNewConfig.NumField(); i++ {
		field := rType.Field(i).Name
		switch field {
		case "Timezone":
			if oldConfig.Timezone == newConfig.Timezone {
				continue
			}

			if err := setTimezone(newConfig.Timezone); err != nil {
				return "", err
			}
		}
	}

	return Get()
}

// tzFile determines which timezone file to read from
func tzFile() string {
	tzFile := os.Getenv(tzPathEnvironment)
	if tzFile == "" {
		tzFile = tzPathDefault
	}

	return tzFile
}

// getTimezone returns the current timezone the system is set to or an error
// if it can't.
var getTimezone = func() (timezone string, err error) {
	tz, err := ioutil.ReadFile(tzFile())
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(tz)), nil
}

// setTimezone sets the specified timezone for the system, an error is returned
// if it can't.
var setTimezone = func(timezone string) error {
	return ioutil.WriteFile(tzFile(), []byte(timezone), 0644)
}
