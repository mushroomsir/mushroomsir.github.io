---
layout: post
title: "Go中原始套接字的深度实践"
excerpt: "原始套接字（raw socket）是一种网络套接字，允许直接发送/接收更底层的数据包而不需要任何传输层协议格式。平常我们使用较多的套接字（socket）都是基于传输层，发送/接收的数据包都是不带TCP/UDP等协议头部的。  当使用套接字发送数据时，传输层在数据包前填充上面格式的"
comments: true
tags:
  - Go
---

* [1\. 介绍](#1-%E4%BB%8B%E7%BB%8D)
* [2\. 传输层socket](#2-%E4%BC%A0%E8%BE%93%E5%B1%82socket)
  * [2\.1 ICMP](#21-icmp)
  * [2\.2 TCP](#22-tcp)
  * [2\.3 传输层协议](#23-%E4%BC%A0%E8%BE%93%E5%B1%82%E5%8D%8F%E8%AE%AE)
* [3\. 网络层socket](#3-%E7%BD%91%E7%BB%9C%E5%B1%82socket)
  * [3\.1 使用Go库](#31-%E4%BD%BF%E7%94%A8go%E5%BA%93)
  * [3\.2 系统调用](#32-%E7%B3%BB%E7%BB%9F%E8%B0%83%E7%94%A8)
  * [3\.3 网络层协议](#33-%E7%BD%91%E7%BB%9C%E5%B1%82%E5%8D%8F%E8%AE%AE)
* [4\. 总结](#4-%E6%80%BB%E7%BB%93)
  * [4\.2 参考](#42-%E5%8F%82%E8%80%83)

## 1. 介绍
原始套接字（raw socket）是一种网络套接字，允许直接发送/接收更底层的数据包而不需要任何传输层协议格式。平常我们使用较多的套接字（socket）都是基于传输层，发送/接收的数据包都是不带TCP/UDP等协议头部的。  
当使用套接字发送数据时，传输层在数据包前填充上面格式的协议头部数据，然后整个发送到网络层，接收时去掉协议头部，把应用数据抛给上层。如果想自己封装头部或定义协议的话，就需要使用原始套接字，直接向网络层发送数据包。  
为了便于后面理解，这里统一称应用数据为 payload，协议头部为 header，套接字为socket。由于平常使用的socket是建立在传输层之上，并且不可以自定义传输层协议头部的socket，约定称之为应用层socket，它**不需要关心**TCP/UDP协议头部如何封装。这样区分的目的是为了理解raw socket在不同层所能做的事情。

## 2. 传输层socket
根据上面的约定，我们把基于网络层IP协议上并且不可以自定义IP协议头部的socket，称为传输层socket，它**需要关心**传输层协议头部如何封装，**不需要关心**IP协议头部如何封装。它“理论上来说”是可以拦截任何传输层的协议，也可以任意自定义传输层协议，比如自定义个协议叫YCP，那么它就和TCP/UDP/ICMP等协议同级。
### 2.1 ICMP
ICMP协议是一个“错误侦测与回报机制”，其目的是检测网路的连线状况﹐确保连线的准确性﹐就是我们经常使用的Ping命令。我们在Go中实践下，来拦截Ping命令产生的数据流量：

```go
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
```
代码中ListenIP是Go提供的来监听IP网络层流量的API，第一个参数是网络层协议，其实只有IP协议，它可以分为ipV4或ipV6。冒号后面的是子协议，表示监听的是网络层中icmp协议的流量，这个子协议在IP header中字段`Protocol(下面的8位协议)`体现出，IP header一般也是20字节：

![ip-header-2.jpg](/assets/img/ip-header-2.jpg)   

这个子协议有200多种，在Go中目前只支持常见几个:icmp，igmp，tcp，udp，ipv6-icmp。
运行程序，在另外个机器里ping 172.17.0.3：
```sh
root@43b16fbeea3d:~# ping 172.17.0.3
PING 172.17.0.3 (172.17.0.3) 56(84) bytes of data.
64 bytes from 172.17.0.3: icmp_seq=1 ttl=64 time=0.078 ms
64 bytes from 172.17.0.3: icmp_seq=2 ttl=64 time=0.085 ms
64 bytes from 172.17.0.3: icmp_seq=3 ttl=64 time=0.389 ms
```
本机监听到Ping如下：
```sh
root@2de84a6c1fed:/go/src/github.com/mushroomsir/blog/examples/001/transport# go run main.go
64 172.17.0.2 echo 0 15729
64 172.17.0.2 echo 0 47698
64 172.17.0.2 echo 0 56243
64 172.17.0.2 echo 0 2072
64 172.17.0.2 echo 0 62072
```
### 2.2 TCP
监控TCP只需要把ICMP换成TCP即可，表示监听的是网络层中TCP协议的流量：
```go
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
```
因为监控的是TCP流量，所以数据都会有TCP的header。NewTCPHeader是一个分析TCP header的struct，在示例代码中有。当运行这段程序时，是可以监控到所有到达本机`172.17.0.3`这块网卡的数据的。在另外台机器运行：
```sh
root@43b16fbeea3d:~# curl 172.17.0.3:80
curl: (7) Failed to connect to 172.17.0.3 port 80: Connection refused
```
或者
```sh
root@43b16fbeea3d:~# curl 172.17.0.3:8000
curl: (7) Failed to connect to 172.17.0.3 port 8000: Connection refused
```
本机监听到如下：
```sh
root@2de84a6c1fed:/go/src/github.com/mushroomsir/blog/examples/001/transporttcp# go run main.go tcp.go
40 172.17.0.2 Source=54482 Destination=80 SeqNum=3189186693 AckNum=0 DataOffset=10 Reserved=0 ECN=0 Ctrl=2 Window=29200 Checksum=22614 Urgent=[] Options=%!v(MISSING)
40 172.17.0.2 Source=56928 Destination=8000 SeqNum=2042858949 AckNum=0 DataOffset=10 Reserved=0 ECN=0 Ctrl=2 Window=29200 Checksum=22614 Urgent=[] Options=%!v(MISSING)
```
可以看到本机已经成功拦截到来自```172.17.0.2```的请求。TCP header中Source是源端口，Destination是目标端口，
因为监听的是IPv4协议上的所有TCP流量，所以不管目标端口是80或8000，都能接收到。直接用浏览器访问也是可以的：
```sh
40 172.17.0.1 Source=34830 Destination=8020 SeqNum=2212492703 AckNum=0 DataOffset=10 Reserved=0 ECN=0 Ctrl=2 Window=29200 Checksum=22613 Urgent=[] Options=%!v(MISSING)
```
但结果和curl一样报错，因为本机虽然监听到了，但并没有做任何处理，比如TCP三次握手都没有完成。如果想自己封装个TCP，那就必须按照TCP协议完成三次握手，只处理本端口的流量数据等。下图是TCP header中的各字段：

![tcp-header.jpg](/assets/img/tcp-header.jpg) 

### 2.3 传输层协议
让我们来自定义个传输层YCP协议，参考TCP Header，定义YCP Header，然后发送`xxx`payload。
客户端代码如下：
```go
func main() {
	local := "127.0.0.1"
	remote := "172.17.0.3"
	conn, _ := net.Dial("ip4:tcp", remote)
	ycpHeader:= util.TCPHeader{
		Source:      17663, 
		Destination: 8020,
		SeqNum:      2,
		AckNum:      0,
		DataOffset:  5,      
		Reserved:    0,      
		ECN:         0,      
		Ctrl:        2,      
		Window:      0xaaaa, 
		Checksum:    0,      
		Urgent:      99,
	}
	data := ycpHeader.Marshal()
	ycpHeader.Checksum = util.Csum(data, to4byte(local), to4byte(remote))
	data = ycpHeader.Marshal()
	data=append(data,[]byte("xxx")...)
	conn.Write(data)
}
```
服务端代码如下：
```go
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
```
启动服务端，然后运行客户端，服务端输出：
```sh
root@2de84a6c1fed:/go/src/github.com/mushroomsir/blog/examples/001/transportcustom/server#
go run main.go
23 172.17.0.2 Source=17663 Destination=8020 SeqNum=2 AckNum=0 DataOffset=5 Reserved=0 ECN=0 Ctrl=2 Window=43690 Checksum=30058 Urgent=99 xxx
```
可以看到Urgent是99，发送时故意定义的大值，然后playload是```xxx```。
## 3. 网络层socket
### 3.1 使用Go库
根据上面的约定，我们把基于网络层IP协议上并且可以自定义IP协议头部的socket，称为网络层socket，它**需要关心**IP协议头部如何封装，**不需要关心**以太网帧的头部和尾部如何封装。来看下面例子：
```go
func main() {
	netaddr, _ := net.ResolveIPAddr("ip4", "172.17.0.3")
	conn, _ := net.ListenIP("ip4:tcp", netaddr)
	ipconn,_:=ipv4.NewRawConn(conn)
	for {
		buf := make([]byte, 1480)
		hdr, payload, controlMessage, _ := ipconn.ReadFrom(buf)
		fmt.Println("ipheader:",hdr,controlMessage)
		tcpheader:=NewTCPHeader(payload)
		fmt.Println("tcpheader:",tcpheader)
	}
}
```
相比传输层socket而言，需要把传输层拿到的socket转成网络层ip的socket，也就是代码中的NewRawConn，这个函数主要是给这个raw socket启用IP_HDRINCL选项。如果启用的话就会在payload前面提供ip header数据。 然后解析IP header信息:
```
其IP的payload=TCP Header+ TCP payload
```
所以还需要解析TCP header。然后在另外台机器curl验证下：
```sh
root@43b16fbeea3d:~# curl 172.17.0.3:8000
curl: (7) Failed to connect to 172.17.0.3 port 8000: Connection refused
```
本机监听输出：
```sh
root@2de84a6c1fed:/go/src/github.com/mushroomsir/blog/examples/001/network# go run main.go tcp.go
ipheader: ver=4 hdrlen=20 tos=0x0 totallen=60 id=0xd7d1 flags=0x2 fragoff=0x0 ttl=64 proto=6 cksum=0xac3 src=172.17.0.2 dst=172.17.0.3 <nil>
tcpheader: Source=56968 Destination=8000 SeqNum=1824143864 AckNum=0 DataOffset=10 Reserved=0 ECN=0 Ctrl=2 Window=29200 Checksum=22614 Urgent=[] Options=%!v(MISSING)
^Csignal: interrupt
```
### 3.2 系统调用
如果觉得Go库使用起来有限制的话，还可以用system call的方式调用：
```go
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
```
Go库本身也是利用syscall.Socket，来提供raw socket的能力，并封装了一层更易于使用的API。其各参数代表：
**第一个参数：**
1. syscall.AF_INET，表示服务器之间的网络通信
2. syscall.AF_UNIX表示同一台机器上的进程通信
3. syscall.AF_INET6表示以IPv6的方式进行服务器之间的网络通信
4. 其他  

**第二个参数**
1. syscall.SOCK_RAW，表示使用原始套接字，可以构建传输层的协议头部，启用IP_HDRINCL的话，IP层的协议头部也可以构造，就是上面区分的传输层socket和网络层socket。
2. syscall.SOCK_STREAM, 基于TCP的socket通信，应用层socket。
3. syscall.SOCK_DGRAM, 基于UDP的socket通信，应用层socket。
4. 其他

**第三个参数**  
即ICMP章节提到的子协议号，操作系统内核发现接收到的IP header中的协议号与创建时填的协议号一样时，就交给上层处理。
1. IPPROTO_TCP     接收TCP协议的数据
2. IPPROTO_IP      接收任何的IP数据包
3. IPPROTO_UDP     接收UDP协议的数据
4. IPPROTO_ICMP    接收ICMP协议的数据
5. IPPROTO_RAW     只能用来发送IP数据包，不能接收数据。
6. 其他

在另外台机器curl：
```sh
root@43b16fbeea3d:~# curl 172.17.0.3:8999
curl: (7) Failed to connect to 172.17.0.3 port 8999: Connection refused
```
本机监听输出：
```sh
root@2de84a6c1fed:/go/src/github.com/mushroomsir/blog/examples/001/network/systemcall# go run main.go
ipheader: ver=4 hdrlen=20 tos=0x0 totallen=60 id=0x4cb6 flags=0x2 fragoff=0x0 ttl=64 proto=6 cksum=0x95de src=172.17.0.2 dst=172.17.0.3
tcpheader: Source=49484 Destination=8999 SeqNum=3080655072 AckNum=0 DataOffset=10 Reserved=0 ECN=0 Ctrl=2 Window=29200 Checksum=22614 Urgent=[] Options=%!v(MISSING)
```
### 3.3 网络层协议
让我们来自定义个网络层YIP协议，参考IP Header，定义YIP Header，然后发送个空payload。
客户端代码如下：
```go
func main() {
	fd, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
	addr := syscall.SockaddrInet4{
		Port: 0,
		Addr: [4]byte{172, 17, 0, 3},
	}
	yipHeader := ipv4.Header{
		Version:  4,
		Len:      20,
		TotalLen: 20, // 20 bytes for IP
		TTL:      64,
		Protocol: 6, // TCP
		Dst:      net.IPv4(172, 17, 0, 3),
		Src:      net.IPv4(172, 17, 0, 99),
	}
	payload, _ := yipHeader.Marshal()
	syscall.Sendto(fd, payload, 0, &addr)
}
```
服务端代码如下：
```go
func main() {
	netaddr, _ := net.ResolveIPAddr("ip4", "172.17.0.3")
	conn, _ := net.ListenIP("ip4:tcp", netaddr)
	ipconn, _ := ipv4.NewRawConn(conn)
	for {
		buf := make([]byte, 1500)
		hdr, payload, controlMessage, _ := ipconn.ReadFrom(buf)
		fmt.Println("ipheader:", hdr,payload, controlMessage)
	}
}
```
启动服务端监听，运行客户端程序，服务端输出：
```sh
root@2de84a6c1fed:/go/src/github.com/mushroomsir/blog/examples/001/networkcustom/server# go run main.go
ipheader: ver=4 hdrlen=20 tos=0x0 totallen=20 id=0x1363 flags=0x0 fragoff=0x0 ttl=64 proto=6 cksum=0xef9 src=172.17.0.99 dst=172.17.0.3 [] <nil>
```
在客户端示例中，头部填写了源IP是`172.17.0.99`，但实际当中笔者电脑并没有这个IP。假设同一个局域网中，利用raw socket就可以伪装成别人的IP去发消息。

## 4. 总结
基于Raw socket可以把UDP的流量伪装成TCP，这样就不会被ISP封杀。也可以伪装IP去DDOS别人，但基于安全的考虑：Windows并不允许通过Raw socket去发送TCP数据，UDP中的源IP也必须在本地网络接口中能找到才行，监听TCP的流量也是不允许的。  
文中例子代码都在[github-examples](https://github.com/mushroomsir/blog/tree/master/examples/001)，有兴趣的同学可以自己尝试下。按照约定的划分，还有一层链路层socket，这个后面再写。
### 4.2 参考
http://man7.org/linux/man-pages/man7/raw.7.html  
http://man7.org/linux/man-pages/man7/ip.7.html   
https://github.com/golang/net  
https://www.darkcoding.net/software/raw-sockets-in-go-link-layer/  