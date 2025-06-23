package cluster_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	_ "net/http/pprof"

	"github.com/snapcore/snapd/cluster"
	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
)

// TestAssemble runs through the entire assemble process. This test probably
// won't stay like this in its current form.
func TestAssemble(t *testing.T) {
	go http.ListenAndServe("localhost:6060", nil)

	const total = 4

	peers := make([]string, 0, total)
	for i := 0; i < total; i++ {
		peers = append(peers, fmt.Sprintf("127.0.0.1:%d", 8001+i))
	}

	discover := func(ctx context.Context) ([]string, error) {
		return peers, nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	}))

	debug := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	collected := make([]assemblestate.Routes, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		i := i
		wg.Add(1)

		l := logger
		if i == 0 {
			l = debug
		}

		go func() {
			defer wg.Done()

			rtd := strconv.Itoa(i)
			st := state.New(nil)
			routes, err := cluster.Assemble(st, ctx, discover, cluster.AssembleOpts{
				Secret:      "secret",
				ListenIP:    net.ParseIP("127.0.0.1"),
				ListenPort:  8001 + i,
				RDTOverride: rtd,
				Logger:      l,
			})
			if err != nil {
				t.Errorf("assemble failed: %v", err)
			}

			collected[i] = routes
		}()
	}

	wg.Wait()

	expected := connectedGraph(total)

	var count int
	for _, got := range collected {
		if !reflect.DeepEqual(expected, got) {
			count++
		}
	}

	if count > 0 {
		t.Errorf("%d nodes did not get the full graph of routes", count)
	}
}

func connectedGraph(total int) assemblestate.Routes {
	devs := make([]string, 0, total)
	for i := 0; i < total; i++ {
		devs = append(devs, strconv.Itoa(i))
	}
	sort.Strings(devs)

	devices := make([]assemblestate.RDT, 0, total)
	for _, s := range devs {
		devices = append(devices, assemblestate.RDT(s))
	}

	addrs := make([]string, 0, total)
	for i := 0; i < total; i++ {
		addrs = append(addrs, net.JoinHostPort("127.0.0.1", strconv.Itoa(8001+i)))
	}
	sort.Strings(addrs)

	addrIndex := make(map[string]int, total)
	for i, a := range addrs {
		addrIndex[a] = i
	}

	addrForRDT := make(map[string]string, total)
	for i := 0; i < total; i++ {
		addrForRDT[strconv.Itoa(i)] = net.JoinHostPort("127.0.0.1", strconv.Itoa(8001+i))
	}

	var routes []int
	for fi, from := range devs {
		for ti, to := range devs {
			if from == to {
				continue
			}
			routes = append(routes, fi, ti, addrIndex[addrForRDT[to]])
		}
	}

	return assemblestate.Routes{
		Devices:   devices,
		Addresses: addrs,
		Routes:    routes,
	}
}

type fileBackend struct {
	path string
}

func (fb *fileBackend) Checkpoint(data []byte) error {
	return osutil.AtomicWriteFile(fb.path, data, 0600, 0)
}

func (fb *fileBackend) EnsureBefore(d time.Duration) {
	panic("unexpected call")
}
