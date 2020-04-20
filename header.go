package socks5

import (
	"fmt"
	"io"
	"net"
	"strconv"
)

const (
	// protocol version
	socks4Version = uint8(4)
	socks5Version = uint8(5)
	// request command
	ConnectCommand   = uint8(1)
	BindCommand      = uint8(2)
	AssociateCommand = uint8(3) // udp associate
	// address type
	ipv4Address = uint8(1)
	fqdnAddress = uint8(3) // domain
	ipv6Address = uint8(4)
)

//
const (
	successReply uint8 = iota
	serverFailure
	ruleFailure
	networkUnreachable
	hostUnreachable
	connectionRefused
	ttlExpired
	commandNotSupported
	addrTypeNotSupported
	// 0x09 - 0xff unassigned
)

// head len defined
const (
	// common fields
	reqProtocolVersionBytePos = uint8(0) // proto version pos
	reqCommandBytePos         = uint8(1)
	reqAddrBytePos            = uint8(4)
	reqStartLen               = uint8(4)

	headVERLen      = 1
	headCMDLen      = 1
	headRSVLen      = 1
	headATYPLen     = 1
	headPORTLen     = 2
	headFQDNAddrLen = 1
	reqFQDNAddr     = 249
)

// AddrSpec is used to return the target AddrSpec
// which may be specified as IPv4, IPv6, or a FQDN
type AddrSpec struct {
	FQDN string
	IP   net.IP
	Port int
}

func (a *AddrSpec) String() string {
	if a.FQDN != "" {
		return fmt.Sprintf("%s (%s):%d", a.FQDN, a.IP, a.Port)
	}
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// Address returns a string suitable to dial; prefer returning IP-based
// address, fallback to FQDN
func (a AddrSpec) Address() string {
	if 0 != len(a.IP) {
		return net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port))
	}
	return net.JoinHostPort(a.FQDN, strconv.Itoa(a.Port))
}

// Header represents the SOCKS5/SOCKS4 header, it contains everything that is not payload
type Header struct {
	// Version of socks protocol for message
	Version uint8
	// Socks Command "connect","bind","associate"
	Command uint8
	// Reserved byte
	Reserved uint8 // only socks5 support
	// Address in socks message
	Address AddrSpec
	// private stuff set when Header parsed
	addrType uint8
}

func Parse(r io.Reader) (hd Header, err error) {
	// Read the version and command
	tmp := make([]byte, headVERLen+headCMDLen)
	if _, err = io.ReadFull(r, tmp); err != nil {
		return hd, fmt.Errorf("failed to get header version and command, %v", err)
	}
	hd.Version = tmp[0]
	hd.Command = tmp[1]

	if hd.Version != socks5Version && hd.Version != socks4Version {
		return hd, fmt.Errorf("unrecognized SOCKS version[%d]", hd.Version)
	}
	if hd.Command != ConnectCommand && hd.Command != BindCommand && hd.Command != AssociateCommand {
		return hd, fmt.Errorf("unrecognized command[%d]", hd.Command)
	}
	if hd.Version == socks4Version && hd.Command == AssociateCommand {
		return hd, fmt.Errorf("wrong version for command")
	}

	if hd.Version == socks4Version {
		// read port and ipv4 ip
		tmp = make([]byte, headPORTLen+net.IPv4len)
		if _, err = io.ReadFull(r, tmp); err != nil {
			return hd, fmt.Errorf("failed to get socks4 header port and ip, %v", err)
		}
		hd.Address.Port = buildPort(tmp[0], tmp[1])
		hd.Address.IP = tmp[2:]
	} else if hd.Version == socks5Version {
		tmp = make([]byte, headRSVLen+headATYPLen)
		if _, err = io.ReadFull(r, tmp); err != nil {
			return hd, fmt.Errorf("failed to get header RSV and address type, %v", err)
		}
		hd.Reserved = tmp[0]
		hd.addrType = tmp[1]
		switch hd.addrType {
		case fqdnAddress:
			if _, err = io.ReadFull(r, tmp[:1]); err != nil {
				return hd, fmt.Errorf("failed to get header, %v", err)
			}
			addrLen := int(tmp[0])
			addr := make([]byte, addrLen+2)
			if _, err = io.ReadFull(r, addr); err != nil {
				return hd, fmt.Errorf("failed to get header, %v", err)
			}
			hd.Address.FQDN = string(addr[:addrLen])
			hd.Address.Port = buildPort(addr[addrLen], addr[addrLen+1])
		case ipv4Address:
			addr := make([]byte, net.IPv4len+2)
			if _, err = io.ReadFull(r, addr); err != nil {
				return hd, fmt.Errorf("failed to get header, %v", err)
			}
			hd.Address.IP = addr[:net.IPv4len]
			hd.Address.Port = buildPort(addr[net.IPv4len], addr[net.IPv4len+1])
		case ipv6Address:
			addr := make([]byte, net.IPv6len+2)
			if _, err = io.ReadFull(r, addr); err != nil {
				return hd, fmt.Errorf("failed to get header, %v", err)
			}
			hd.Address.IP = addr[:net.IPv6len]
			hd.Address.Port = buildPort(addr[net.IPv6len], addr[net.IPv6len+1])
		default:
			return hd, unrecognizedAddrType
		}
	}
	return hd, nil
}

func (h Header) Bytes() (b []byte) {
	b = append(b, h.Version)
	b = append(b, h.Command)
	hiPort, loPort := breakPort(h.Address.Port)
	if h.Version == socks4Version {
		b = append(b, hiPort, loPort)
		b = append(b, h.Address.IP...)
	} else if h.Version == socks5Version {
		b = append(b, h.Reserved)
		b = append(b, h.addrType)
		if h.addrType == fqdnAddress {
			b = append(b, byte(len(h.Address.FQDN)))
			b = append(b, []byte(h.Address.FQDN)...)
		} else if h.addrType == ipv4Address {
			b = append(b, h.Address.IP.To4()...)
		} else if h.addrType == ipv6Address {
			b = append(b, h.Address.IP.To16()...)
		}
		b = append(b, hiPort, loPort)
	}
	return b
}

func buildPort(hi, lo byte) int {
	return (int(hi) << 8) | int(lo)
}

func breakPort(port int) (hi, lo byte) {
	return byte(port >> 8), byte(port)
}