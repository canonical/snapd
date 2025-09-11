package utils

import (
	"context"
	"fmt"
	"math"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cakturk/go-netstat/netstat"
	"github.com/snapcore/snapd/client"
)

func getSnapNamePublisherIDFromPID(pid int) (string, string, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command")
    output, err := cmd.Output()
    if err != nil {
        return "", "", err
    }

	trimmedOutput := strings.TrimPrefix(string(output), "COMMAND\n")

	commands := strings.Split(trimmedOutput, " ")

	var snapName string
	for _, command := range commands {
		levels := strings.Split(command, "/")[1:]

		if len(levels) < 2 {
			continue
		}

		if levels[0] == "snap" {
			snapName = levels[1]
			break
		}

	}

	if snapName == "" {
		return "", "", fmt.Errorf("could not find snap in commmand %s", string(output))
	}

	if snapName == "landscape-client" {
		return "canonical", "landscape-client", nil
	}

	snapClient := client.New(nil)
	snapInfo, _, err := snapClient.FindOne(snapName)
	if err != nil {
		return "", "", err
	} else if snapInfo.Publisher.Username != "" {
		return snapInfo.Publisher.Username, snapName, nil
	} else {
		return snapInfo.Publisher.ID, snapName, nil
	}
}

// Helper function used to add process name to context for later use by handler.
// Uses netstat to find the process sending over this connection.
func GetSnapInfoFromConn(addr string) (string, string, error) {
	var host, port_str string
	var err error
	host, port_str, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}

	port_64, err := strconv.ParseUint(port_str, 10, 16)
	if err != nil {
		return "", "", err
	}
	port := uint16(port_64)
	ip := net.ParseIP(host)

	connsTCP, err := netstat.TCPSocks(func(s *netstat.SockTabEntry) bool {
		return s.LocalAddr.IP.Equal(ip) && s.LocalAddr.Port == port
	})
	if err != nil {
		return "", "", err
	}

	connsTCP6, err := netstat.TCP6Socks(func(s *netstat.SockTabEntry) bool {
		return s.LocalAddr.IP.Equal(ip) && s.LocalAddr.Port == port
	})
	if err != nil {
		return "", "", err
	}

	conns := append(connsTCP, connsTCP6...)

	if len(conns) != 1 {
		return "", "", fmt.Errorf("invalid number of matching connections found, must be 1")
	}

	if conns[0].Process == nil {
		return "", "", fmt.Errorf("process info is nil")
	}

	return getSnapNamePublisherIDFromPID(conns[0].Process.Pid)
}

func GetDeviceId() (string, error) {
	snapClient := client.New(nil)

	results, err := snapClient.Known("serial", make(map[string]string), nil)

	for i := 0; i < 5; i++ {
		if err == nil && len(results) == 1 {
			break
		}

		time.Sleep(10 * time.Second * time.Duration(math.Pow(2, float64(i))))
		results, err = snapClient.Known("serial", make(map[string]string), nil)
    }

	if err != nil {
		return "", err
	} else if len(results) == 0 {
		return "", fmt.Errorf("no device-id was returned")
	}

	deviceId := results[0].HeaderString("brand-id") + "." + results[0].HeaderString("model") + "." +
		results[0].HeaderString("serial")

	return deviceId, nil
}

// netPipe simulates a network link using a real connection.
// This is used over net.Pipe because net.Pipe is synchronous and this can create confusing results
// because it does not work like a real network connection (a call to `Write` will block until the other
// end calls `Read` whereas with a real connection there are buffers etc).
// There are many ways to do this, but using a real connection is simple and effective!
func NetPipe(ctx context.Context) (net.Conn, net.Conn, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0") // Port 0 is wildcard port; OS will choose port for us
	if err != nil {
		return nil, nil, err
	}
	defer l.Close()
	var d net.Dialer
	userCon, err := d.DialContext(ctx, "tcp", l.Addr().String()) // Dial the port we just listened on
	if err != nil {
		return nil, nil, err
	}
	ourCon, err := l.Accept() // Should return immediately
	if err != nil {
		userCon.Close()
		return nil, nil, err
	}
	return userCon, ourCon, nil
}
