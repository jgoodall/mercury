package common

import (
	"net"
	"strconv"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// ParsePacket will parse key fields out of a packet.
func ParsePacket(packet gopacket.Packet) (vers uint8, sMAC, dMAC net.HardwareAddr, sIP, dIP net.IP, sPort, dPort uint16, proto uint8, protoStr string) {
	// If this an ethernet packet, continue
	ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
	if ethernetLayer != nil {

		if ip4Layer := packet.Layer(layers.LayerTypeIPv4); ip4Layer != nil {
			vers = uint8(4)
		}
		if ip6Layer := packet.Layer(layers.LayerTypeIPv6); ip6Layer != nil {
			vers = uint8(6)
		}
		ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)

		// Set MAC address
		sMAC = ethernetPacket.SrcMAC
		dMAC = ethernetPacket.DstMAC

		// If there is a network layer, get IPs and (for tcp and udp) ports
		if n := packet.NetworkLayer(); n != nil {
			flow := n.NetworkFlow()
			src, dst := flow.Endpoints()
			sIP = net.ParseIP(src.String())
			dIP = net.ParseIP(dst.String())

			tcp := packet.Layer(layers.LayerTypeTCP)
			if tcp != nil {
				proto = uint8(6)
				protoStr = "TCP"
			}
			udp := packet.Layer(layers.LayerTypeUDP)
			if udp != nil {
				proto = uint8(17)
				protoStr = "UDP"
			}
			icmp := packet.Layer(layers.LayerTypeICMPv4)
			if icmp != nil {
				proto = uint8(1)
				protoStr = "ICMP"
			}
			icmp6 := packet.Layer(layers.LayerTypeICMPv6)
			if icmp6 != nil {
				proto = uint8(58)
				protoStr = "ICMPv6"
			}
			if tcp != nil || udp != nil {
				src, dst := packet.TransportLayer().TransportFlow().Endpoints()
				sp, _ := strconv.ParseUint(src.String(), 10, 16)
				sPort = uint16(sp)
				dp, _ := strconv.ParseUint(dst.String(), 10, 16)
				dPort = uint16(dp)
			}
		}
	}
	return
}
