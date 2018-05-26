package main

import (
	"fmt"
	"github.com/mushroomsir/blog/examples/util"
	"net"
)

func main() {
	netaddr, _ := net.ResolveIPAddr("ip4", "172.17.0.3")
	conn, _ := net.ListenIP("ip4:tcp", netaddr)
	for {
		buf := make([]byte, 1480)
		n, addr, _ := conn.ReadFrom(buf)
		ycpheader := util.NewTCPHeader(buf[0:20])
		fmt.Println(n, addr, ycpheader, string(buf[20:23]))
	}
}
