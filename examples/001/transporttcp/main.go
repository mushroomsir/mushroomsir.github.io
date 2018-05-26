package main
import (
	"net"
	"fmt"
)
func main() {
	netaddr, _ := net.ResolveIPAddr("ip4", "172.17.0.3")
	conn, _ := net.ListenIP("ip4:tcp", netaddr)
	for {
		buf := make([]byte, 1480)
		n, addr, _ := conn.ReadFrom(buf)
		tcpheader:=NewTCPHeader(buf[0:n])
		fmt.Println(n,addr,tcpheader)
	}
}