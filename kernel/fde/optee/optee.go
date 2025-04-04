//go:build !arm && !arm64

package optee

import (
	"errors"
)

func TAPresent() bool {
	return false
}

func DecryptKey(input []byte, handle []byte) ([]byte, error) {
	return nil, errors.New("unsupported platform")
}

func EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	return nil, nil, errors.New("unsupported platform")
}

func LockTA() error {
	return errors.New("unsupported platform")
}
