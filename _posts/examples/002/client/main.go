package main

import (
	"flag"
	"net"
	"syscall"
	"time"

	"github.com/mushroomsir/go-examples/ethernet/wire"
	"github.com/mushroomsir/go-examples/util"
	"github.com/mushroomsir/logger/alog"
)

var (
	iface = flag.String("iface", "eth0", "net interface name")
	ohter = net.HardwareAddr{0x02, 0x42, 0xac, 0x11, 0x00, 0x02}
)

var etherType uint16 = 52428

//syscall.ETH_P_IP

func main() {
	flag.Parse()
	ifi, err := net.InterfaceByName(*iface)
	util.CheckError(err)
	alog.Info(ifi)
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(wire.Htons(etherType)))
	util.CheckError(err)
	for {
		payload := []byte("msg")
		minPayload := len(payload)
		if minPayload < 46 {
			minPayload = 46
		}
		b := make([]byte, 14+minPayload)
		header := &wire.Header{
			DestinationAddress: ohter,
			SourceAddress:      ifi.HardwareAddr,
			EtherType:          etherType,
		}
		copy(b[0:14], header.Marshal())
		copy(b[14:14+len(payload)], payload)

		var baddr [8]byte
		copy(baddr[:], ohter)
		to := &syscall.SockaddrLinklayer{
			Ifindex:  ifi.Index,
			Halen:    6,
			Addr:     baddr,
			Protocol: wire.Htons(etherType),
		}
		err = syscall.Sendto(fd, b, 0, to)
		util.CheckError(err)
		time.Sleep(time.Second)
	}
}
