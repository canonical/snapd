//go:build !linux || (!arm && !arm64)

package optee

import (
	"errors"
)

type unsupportedClient struct{}

var ErrUnsupportedPlatform = errors.New("unsupported platform")

func (c *unsupportedClient) FDETAPresent() bool {
	return false
}

func (c *unsupportedClient) DecryptKey(input []byte, handle []byte) ([]byte, error) {
	return nil, ErrUnsupportedPlatform
}

func (c *unsupportedClient) EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	return nil, nil, ErrUnsupportedPlatform
}

func (c *unsupportedClient) LockTA() error {
	return ErrUnsupportedPlatform
}

func (c *unsupportedClient) Version() (string, error) {
	return "", ErrUnsupportedPlatform
}

func newOPTEEClient() Client {
	return &unsupportedClient{}
}
