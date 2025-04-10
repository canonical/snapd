//go:build !linux || (!arm && !arm64)

package optee

import (
	"errors"
)

func fdeTAPresentImpl() bool {
	return false
}

func decryptKeyImpl(input []byte, handle []byte) ([]byte, error) {
	return nil, errors.New("unsupported platform")
}

func encryptKeyImpl(input []byte) (handle []byte, sealed []byte, err error) {
	return nil, nil, errors.New("unsupported platform")
}

func lockTAImpl() error {
	return errors.New("unsupported platform")
}
