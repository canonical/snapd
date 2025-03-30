//go:build !arm && !arm64

package optee

import (
	"errors"
)

func DecryptKey(input []byte, handle []byte) ([]byte, error) {
	return nil, errors.New("error: unsupported platform")
}

func EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	return nil, nil, errors.New("error: unsupported platform")
}

func LockTA() error {
	return errors.New("error: unsupported platform")
}
