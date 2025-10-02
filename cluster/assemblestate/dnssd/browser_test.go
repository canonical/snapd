package dnssd

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowse(t *testing.T) {
	testIface, _ := net.InterfaceByName("lo0")
	if testIface == nil {
		testIface, _ = net.InterfaceByName("lo")
	}
	if testIface == nil {
		t.Fatal("can not find the local interface")
	}

	localhost, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	localhost = strings.TrimSuffix(strings.Replace(localhost, " ", "-", -1), ".local") // replace spaces with dashes and remove .local suffix
	for tName, hostValue := range map[string]string{
		"regular host": "My-Computer",
		"empty host":   "",
		"ip address":   "192.168.0.1",
	} {
		t.Run(tName, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cfg := Config{
				Name:   "My Service",
				Type:   "_test._tcp",
				Host:   hostValue,
				Port:   12334,
				Ifaces: []string{testIface.Name},
			}
			srv, err := NewService(cfg)
			if err != nil {
				t.Fatal(err)
			}
			rs, err := NewResponder()
			if err != nil {
				t.Fatal(err)
			}

			go func() {
				_ = rs.Respond(ctx)
			}()

			_, err = rs.Add(srv)
			if err != nil {
				t.Fatal(err)

			}

			resultChan := make(chan BrowseEntry)
			defer close(resultChan)
			go func() {
				_ = LookupType(ctx, fmt.Sprintf("%s.local.", cfg.Type), func(entry BrowseEntry) {
					resultChan <- entry
				}, func(entry BrowseEntry) {})
			}()

			select {
			case <-ctx.Done():
				t.Fatal("timeout")
			case entry := <-resultChan:
				if entry.Name != cfg.Name {
					t.Fatalf("is=%v want=%v", entry.Name, cfg.Name)
				}
				if tName == "empty host" {
					if entry.Host != localhost {
						t.Fatalf("is=%v want=%v", entry.Host, localhost)
					}
				} else {
					if entry.Host != cfg.Host {
						t.Fatalf("is=%v want=%v", entry.Host, cfg.Host)
					}
				}
				if entry.Port != cfg.Port {
					t.Fatalf("is=%v want=%v", entry.Port, cfg.Port)
				}
			}
		})
	}
}
