package main

import (
	"golang.org/x/net/icmp"
	"fmt"
	"net"
)

func main() {
	netaddr, _ := net.ResolveIPAddr("ip4", "172.17.0.3")
	conn, _ := net.ListenIP("ip4:icmp", netaddr)
	for {
		buf := make([]byte, 1024)
		n, addr, _ := conn.ReadFrom(buf)
		msg,_:=icmp.ParseMessage(1,buf[0:n])
		fmt.Println(n, addr, msg.Type,msg.Code,msg.Checksum)
	}
}