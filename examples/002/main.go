package main

import (
	"flag"
	"fmt"
	"net"
	"syscall"

	"github.com/mushroomsir/go-examples/ethernet/wire"
	"github.com/mushroomsir/go-examples/util"
	"github.com/mushroomsir/logger/alog"
)

var (
	iface = flag.String("iface", "eth0", "net interface name")
)

// https://github.com/spotify/linux/blob/master/include/linux/if_ether.h
// http://man7.org/linux/man-pages/man7/packet.7.html
func main() {
	ifi, err := net.InterfaceByName("eth0")
	util.CheckError(err)
	alog.Info(ifi.HardwareAddr.String())
	// syscall.ETH_P_IP
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(wire.Htons(0xcccc)))
	util.CheckError(err)
	for {
		buf := make([]byte, 1514)
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		util.CheckError(err)
		if n < 14 {
			alog.Warning("the ethernet header length < 14")
			continue
		}
		header := wire.ParseHeader(buf[0:14])
		fmt.Println(header)
		// ip4header, _ := ipv4.ParseHeader(buf[14:34])
		// fmt.Println("ipv4 header: ", ip4header)
		// icmpPayload := buf[34:]
		// msg, _ := icmp.ParseMessage(1, icmpPayload)
		// fmt.Println("icmp: ", msg)
	}
}
