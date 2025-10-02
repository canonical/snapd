package dnssd

import (
	"context"
)

func (r *responder) Debug(ctx context.Context, fn ReadFunc) {
	conn := r.conn.(*mdnsConn)

	readCtx, readCancel := context.WithCancel(ctx)
	defer readCancel()

	ch := conn.read(readCtx)

	for {
		select {
		case req := <-ch:
			fn(req)
		case <-ctx.Done():
			return
		}
	}
}
