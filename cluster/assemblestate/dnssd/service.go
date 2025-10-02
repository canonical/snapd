package dnssd

import (
	"bytes"
	"github.com/brutella/dnssd/log"

	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type Config struct {
	// Name of the service.
	Name string

	// Type is the service type, for example "_hap._tcp".
	Type string

	// Domain is the name of the domain, for example "local".
	// If empty, "local" is used.
	Domain string

	// Host is the name of the host (no trailing dot).
	// If empty the local host name is used.
	Host string

	// Txt records
	Text map[string]string

	// IP addresses of the service.
	// This field is deprecated and should not be used.
	IPs []net.IP

	// Port is the port of the service.
	Port int

	// Interfaces at which the service should be registered
	Ifaces []string

	// The addresses for the interface which should be used (A / AAAA / Both)
	// If empty, all addresses are used.
	AdvertiseIPType IPType
}

func (c Config) Copy() Config {
	return Config{
		Name:            c.Name,
		Type:            c.Type,
		Domain:          c.Domain,
		Host:            c.Host,
		Text:            c.Text,
		IPs:             c.IPs,
		Port:            c.Port,
		Ifaces:          c.Ifaces,
		AdvertiseIPType: c.AdvertiseIPType,
	}
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isWhitespace(r rune) bool {
	return r == ' '
}

// validHostname returns a valid hostname as specified in RFC-952 and RFC1123.
func validHostname(host string) string {
	result := ""
	z := len(host) - 1
	for i, r := range host {
		if isWhitespace(r) {
			r = '-'
		}
		// hostname must start with an alpha [RFC-952 ASSUMPTIONS] or digit [RFC1123 2.1] character.
		if i == 0 && (!isDigit(r) && !isAlpha(r)) {
			log.Debug.Printf(`hostname "%s" starts with "%s"`, host, string(r))
			continue
		}

		// [RFC-952 ASSUMPTIONS] The last character must not be a minus sign or period.
		if i == z && (r == '-' || r == '.') {
			log.Debug.Printf(`hostname "%s" ends with "%s"`, host, string(r))
			continue
		}

		if !isDigit(r) && !isAlpha(r) && r != '-' && r != '.' {
			log.Debug.Printf(`hostname "%s" contains "%s"`, host, string(r))
			continue
		}

		result += string(r)
	}

	return result
}

type IPType int

const (
	Both = IPType(0)
	IPv4 = IPType(4)
	IPv6 = IPType(6)
)

// Service represents a DNS-SD service instance
type Service struct {
	Name            string
	Type            string
	Domain          string
	Host            string
	Text            map[string]string
	TTL             time.Duration // Original time to live
	Port            int
	IPs             []net.IP
	Ifaces          []string
	AdvertiseIPType IPType

	// stores ips by interface name for caching purposes
	ifaceIPs   map[string][]net.IP
	expiration time.Time
}

// NewService returns a new service for the given config.
func NewService(cfg Config) (s Service, err error) {
	name := cfg.Name
	typ := cfg.Type
	port := cfg.Port

	if len(name) == 0 {
		err = fmt.Errorf("invalid name \"%s\"", name)
		return
	}

	if len(typ) == 0 {
		err = fmt.Errorf("invalid type \"%s\"", typ)
		return
	}

	if port == 0 {
		err = fmt.Errorf("invalid port \"%d\"", port)
		return
	}

	domain := cfg.Domain
	if len(domain) == 0 {
		domain = "local"
	}

	host := cfg.Host
	if len(host) == 0 {
		host = hostname()
	}

	text := cfg.Text
	if text == nil {
		text = map[string]string{}
	}

	ips := []net.IP{}
	var ifaces []string

	if cfg.IPs != nil && len(cfg.IPs) > 0 {
		ips = cfg.IPs
	}

	if cfg.Ifaces != nil && len(cfg.Ifaces) > 0 {
		ifaces = cfg.Ifaces
	}

	return Service{
		Name:            trimServiceNameSuffixRight(name),
		Type:            typ,
		Domain:          domain,
		Host:            validHostname(host),
		Text:            text,
		Port:            port,
		IPs:             ips,
		AdvertiseIPType: cfg.AdvertiseIPType,
		Ifaces:          ifaces,
		ifaceIPs:        map[string][]net.IP{},
	}, nil
}

// Interfaces returns the network interfaces for which the service is registered,
// or all multicast network interfaces, if no IP addresses are specified.
func (s *Service) Interfaces() []*net.Interface {
	if len(s.Ifaces) > 0 {
		ifis := []*net.Interface{}
		for _, name := range s.Ifaces {
			if ifi, err := net.InterfaceByName(name); err == nil {
				ifis = append(ifis, ifi)
			}
		}

		return ifis
	}

	return MulticastInterfaces()
}

// IsVisibleAtInterface returns true, if the service is published
// at the network interface with name n.
func (s *Service) IsVisibleAtInterface(n string) bool {
	if len(s.Ifaces) == 0 {
		return true
	}

	for _, name := range s.Ifaces {
		if name == n {
			return true
		}
	}

	return false
}

// IPsAtInterface returns the ip address at a specific interface.
func (s *Service) IPsAtInterface(iface *net.Interface) []net.IP {
	if iface == nil {
		return []net.IP{}
	}

	if ips, ok := s.ifaceIPs[iface.Name]; ok {
		return ips
	}

	if len(s.IPs) > 0 {
		return s.IPs
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return []net.IP{}
	}

	ips := []net.IP{}
	for _, addr := range addrs {
		if ip, _, err := net.ParseCIDR(addr.String()); err == nil {
			ips = append(ips, ip)
		} else {
			log.Debug.Println(err)
		}
	}

	return ips
}

// HasIPOnAnyInterface returns true, if the service defines
// the ip address on any network interface.
func (s *Service) HasIPOnAnyInterface(ip net.IP) bool {
	for _, iface := range s.Interfaces() {
		ips := s.IPsAtInterface(iface)
		for _, ifaceIP := range ips {
			if ifaceIP.Equal(ip) {
				return true
			}
		}
	}

	return false
}

// Copy returns a copy of the service.
func (s Service) Copy() *Service {
	return &Service{
		Name:            s.Name,
		Type:            s.Type,
		Domain:          s.Domain,
		Host:            s.Host,
		Text:            s.Text,
		TTL:             s.TTL,
		IPs:             s.IPs,
		Port:            s.Port,
		AdvertiseIPType: s.AdvertiseIPType,
		Ifaces:          s.Ifaces,
		ifaceIPs:        s.ifaceIPs,
		expiration:      s.expiration,
	}
}

func (s Service) EscapedName() string {
	return escape.Replace(s.Name)
}

func incrementHostname(name string, count int) string {
	return fmt.Sprintf("%s-%d", trimHostNameSuffixRight(name), count)
}

func trimHostNameSuffixRight(name string) string {
	minus := strings.LastIndex(name, "-")
	if minus == -1 || /* not found*/
		minus == len(name)-1 /* at the end */ {
		return name
	}

	// after minus
	after := name[minus+1:]
	for _, r := range after {
		if !isDigit(r) {
			return name
		}
	}

	trimmed := name[:minus]
	if len(trimmed) == 0 {
		return name
	}
	return trimmed
}

// trimServiceNameSuffixRight removes any suffix with the format " (%d)".
func trimServiceNameSuffixRight(name string) string {
	open := strings.LastIndex(name, "(")
	close := strings.LastIndex(name, ")")
	if open == -1 || close == -1 || /* not found*/
		open >= close || /* wrong order */
		open == 0 || /* at the beginning */
		close != len(name)-1 /* not at the end */ {
		return name
	}

	// between brackets are only numbers
	between := name[open+1 : close-1]
	for _, r := range between {
		if !isDigit(r) {
			return name
		}
	}

	// before opening bracket is a whitespace
	if name[open-1] != ' ' {
		return name
	}

	trimmed := name[:open]
	trimmed = strings.TrimRight(trimmed, " ")
	if len(trimmed) == 0 {
		return name
	}
	return trimmed
}

func incrementServiceName(name string, count int) string {
	return fmt.Sprintf("%s (%d)", trimServiceNameSuffixRight(name), count)
}

// EscapedServiceInstanceName returns the same as `ServiceInstanceName()`
// but escapes any special characters.
func (s Service) EscapedServiceInstanceName() string {
	return fmt.Sprintf("%s.%s.%s.", s.EscapedName(), s.Type, s.Domain)
}

// ServiceInstanceName returns the service instance name
// in the form of <instance name>.<service>.<domain>.
// (Note the trailing dot.)
func (s Service) ServiceInstanceName() string {
	return fmt.Sprintf("%s.%s.%s.", s.Name, s.Type, s.Domain)
}

// ServiceName returns the service name in the
// form of "<service>.<domain>."
// (Note the trailing dot.)
func (s Service) ServiceName() string {
	return fmt.Sprintf("%s.%s.", s.Type, s.Domain)
}

// Hostname returns the hostname in the
// form of "<hostname>.<domain>."
// (Note the trailing dot.)

func (s Service) Hostname() string {
	return fmt.Sprintf("%s.%s.", s.Host, s.Domain)
}

// SetHostname sets the service's host name and
// domain (if specified as "<hostname>.<domain>.").
// (Note the trailing dot.)
func (s *Service) SetHostname(hostname string) {
	name, domain := parseHostname(hostname)

	if domain == s.Domain {
		s.Host = name
	}
}

// ServicesMetaQueryName returns the name of the meta query
// for the service domain in the form of "_services._dns-sd._udp.<domain.".
// (Note the trailing dot.)
func (s Service) ServicesMetaQueryName() string {
	return fmt.Sprintf("_services._dns-sd._udp.%s.", s.Domain)
}

func (s *Service) addIP(ip net.IP, iface *net.Interface) {
	s.IPs = append(s.IPs, ip)
	if iface != nil {
		ifaceIPs := []net.IP{ip}
		if ips, ok := s.ifaceIPs[iface.Name]; ok {
			ifaceIPs = append(ips, ip)
		}
		s.ifaceIPs[iface.Name] = ifaceIPs
	}
}

func newService(instance string) *Service {
	name, typ, domain := parseServiceInstanceName(instance)
	return &Service{
		Name:     name,
		Type:     typ,
		Domain:   domain,
		Text:     map[string]string{},
		IPs:      []net.IP{},
		Ifaces:   []string{},
		ifaceIPs: map[string][]net.IP{},
	}
}

var unescape = strings.NewReplacer("\\", "")
var escape *strings.Replacer

func init() {
	specialChars := []byte{'.', ' ', '\'', '@', ';', '(', ')', '"', '\\'}
	replaces := make([]string, 2*len(specialChars))
	for i, char := range specialChars {
		replaces[2*i] = string(char)
		replaces[2*i+1] = "\\" + string(char)
	}
	escape = strings.NewReplacer(replaces...)
}

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < len(r)/2; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// parseServiceInstanceName parses str to get the instance, service and domain name.
func parseServiceInstanceName(str string) (name string, service string, domain string) {
	r := bytes.NewBufferString(reverse(strings.Trim(str, ".")))
	l, err := r.ReadString('.')
	if err != nil {
		return
	}
	domain = strings.Trim(l, ".")
	domain = reverse(domain)

	proto, err := r.ReadString('.')
	if err != nil {
		return
	}
	typee, err := r.ReadString('.')
	if err != nil {
		return
	}
	service = fmt.Sprintf("%s.%s", strings.Trim(reverse(typee), "."), strings.Trim(reverse(proto), "."))
	name = reverse(r.String())
	name = unescape.Replace(name)

	return
}

// Get Fully Qualified Domain Name
// returns "unknown" or hostanme in case of error
func hostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}

	name, _ := parseHostname(hostname)
	return name
}

func parseHostname(str string) (name string, domain string) {
	trimmed := strings.Trim(str, ".")
	last := strings.LastIndex(trimmed, ".")
	if last == -1 {
		name = trimmed
		return
	}

	name = strings.Trim(str[:last], ".")
	domain = strings.Trim(str[last+1:], ".")
	return
}

// MulticastInterfaces returns a list of all active multicast network interfaces.
func MulticastInterfaces(filters ...string) []*net.Interface {
	var tmp []*net.Interface
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	for _, iface := range ifaces {
		iface := iface
		if (iface.Flags & net.FlagUp) == 0 {
			continue
		}

		if (iface.Flags & net.FlagMulticast) == 0 {
			continue
		}

		if !containsIfaces(iface.Name, filters) {
			continue
		}

		// check for a valid ip at that interface
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if _, _, err := net.ParseCIDR(addr.String()); err == nil {
				tmp = append(tmp, &iface)
				break
			}
		}
	}

	return tmp
}

func containsIfaces(iface string, filters []string) bool {
	if filters == nil || len(filters) <= 0 {
		return true
	}

	for _, ifn := range filters {
		if ifn == iface {
			return true
		}
	}

	return false
}
