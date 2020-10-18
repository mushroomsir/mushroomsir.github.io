package main

import (
	"fmt"
	"github.com/mushroomsir/blog/examples/util"
	"golang.org/x/net/ipv4"
	"os"
	"syscall"
)

func main() {
	fd, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_TCP)
	f := os.NewFile(uintptr(fd), fmt.Sprintf("fd %d", fd))
	for {
		buf := make([]byte, 1500)
		f.Read(buf)
		ip4header, _ := ipv4.ParseHeader(buf[:20])
		fmt.Println("ipheader:", ip4header)
		tcpheader := util.NewTCPHeader(buf[20:40])
		fmt.Println("tcpheader:", tcpheader)
	}
}
