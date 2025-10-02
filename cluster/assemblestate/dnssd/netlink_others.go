//go:build !linux

package dnssd

import (
	"context"

	"github.com/brutella/dnssd/log"
)

func (r *responder) linkSubscribe(context.Context) {
	log.Info.Println("dnssd: unable to wait for link updates")
}
