package dnssd

import (
	"net"
	"testing"
)

func TestParseServiceInstanceName(t *testing.T) {
	instance, service, domain := parseServiceInstanceName("Test._hap._tcp.local.")

	if is, want := instance, "Test"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	if is, want := service, "_hap._tcp"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	if is, want := domain, "local"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}
}

func TestParseEscapedServiceInstanceName(t *testing.T) {
	instance, service, domain := parseServiceInstanceName("Home\\ Printer\\ v1\\.0._hap._tcp.local.")

	if is, want := instance, "Home Printer v1.0"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	if is, want := service, "_hap._tcp"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	if is, want := domain, "local"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}
}

func TestParseHostname(t *testing.T) {
	tests := []struct {
		Hostname string
		Name     string
		Domain   string
	}{
		{"Computer.local.", "Computer", "local"},
		{"Computer.local", "Computer", "local"},
		{"Computer.", "Computer", ""},
		{"Computer", "Computer", ""},
		{"192.168.0.1.local", "192.168.0.1", "local"},
	}
	for _, test := range tests {
		name, domain := parseHostname(test.Hostname)
		if name != test.Name {
			t.Fatalf("%s != %s", name, test.Name)
		}

		if domain != test.Domain {
			t.Fatalf("%s != %s", domain, test.Domain)
		}
	}
}

func TestNewServiceWithMinimalConfig(t *testing.T) {
	cfg := Config{
		Name: "Test",
		Type: "_asdf._tcp",
		Port: 1234,
	}

	sv, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if len(sv.Host) == 0 {
		t.Fatal("Expected hostname")
	}

	if is, want := sv.Domain, "local"; is != want {
		t.Fatalf("%v != %v", is, want)
	}

	if is, want := len(sv.IPs), 0; is != want {
		t.Fatalf("%v != %v", is, want)
	}
}

func TestNewServiceWithExplicitIP(t *testing.T) {
	cfg := Config{
		Name: "Test",
		Type: "_asdf._tcp",
		IPs:  []net.IP{net.ParseIP("127.0.0.1")},
		Port: 1234,
	}
	sv, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if is, want := len(sv.IPs), 1; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	if is, want := sv.IPs[0].String(), "127.0.0.1"; is != want {
		t.Fatalf("%v != %v", is, want)
	}
}

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		Hostname string
		Expected string
	}{
		{"macbookpro.local", "macbookpro.local"},
		{"macbookpro (2).local", "macbookpro-2.local"},
		{"macbookpro_2.local", "macbookpro2.local"},
		{"MacBook Pro 2019.local", "MacBook-Pro-2019.local"},
	}

	for _, test := range tests {
		if is, want := validHostname(test.Hostname), test.Expected; is != want {
			t.Fatalf("is=%v want=%v", is, want)
		}
	}
}

func TestIncrementHostName(t *testing.T) {
	tests := []struct {
		Name     string
		Count    int
		Expected string
	}{
		{"my-hostname", 2, "my-hostname-2"},
		{"my-hostname-2", 3, "my-hostname-3"},
		{"my-hostname-asdf", 3, "my-hostname-asdf-3"},
		{"my-hostname-", 3, "my-hostname--3"},
	}

	for _, test := range tests {
		if is, want := incrementHostname(test.Name, test.Count), test.Expected; is != want {
			t.Fatalf("is=%v want=%v", is, want)
		}
	}
}

func TestIncrementServiceName(t *testing.T) {
	tests := []struct {
		Name     string
		Count    int
		Expected string
	}{
		{"My Name", 2, "My Name (2)"},
		{"My Name-2", 2, "My Name-2 (2)"},
		{"My Name (2)", 3, "My Name (3)"},
		{"My Name (2)", 4, "My Name (4)"},
		{"My Name(2)", 4, "My Name(2) (4)"},
	}

	for _, test := range tests {
		if is, want := incrementServiceName(test.Name, test.Count), test.Expected; is != want {
			t.Fatalf("is=%v want=%v", is, want)
		}
	}
}
func TestTrimServiceNameSuffixRight(t *testing.T) {
	tests := []struct {
		Name     string
		Expected string
	}{
		{"My Name", "My Name"},
		{"My Name(2)", "My Name(2)"},
		{"My Name-2", "My Name-2"},
		{"My Name (2)", "My Name"},
		{"My Name (0)", "My Name"},
	}

	for _, test := range tests {
		if is, want := trimServiceNameSuffixRight(test.Name), test.Expected; is != want {
			t.Fatalf("is=%v want=%v (%+v)", is, want, test)
		}
	}
}
