
* [1\. 介绍](#1-%E4%BB%8B%E7%BB%8D)
* [2\. 服务端](#2-%E6%9C%8D%E5%8A%A1%E7%AB%AF)
* [3\. 协议头部](#3-%E5%8D%8F%E8%AE%AE%E5%A4%B4%E9%83%A8)
* [4\. 客户端](#4-%E5%AE%A2%E6%88%B7%E7%AB%AF)
* [5\. 总结](#5-%E6%80%BB%E7%BB%93)

## 1. 介绍
接上次的博客，按照约定的划分，还有一层链路层socket。这一层就可以自定义链路层的协议头部(header)了，下面是目前主流的Ethernet 2(以太网)标准的头部:
![Ethernet_Type_II_Frame_format.png](img/Ethernet_Type_II_Frame_format.png)   
相比IP和TCP的头部，以太网的头部要简单些，仅有目标MAC地址，源MAC地址，数据协议类型(比如常见的IP和ARP协议)。

但多了尾部的FCS(帧校验序列)，用的是CRC校验法。如果校验错误，直接丢弃掉，不会送到上层的协议栈中，链路层只保证数据帧的正确性(丢掉错误的)。具体数据报的完整性由上层控制，比如TCP重传。
链路层最大长度是1518字节，除去18字节的头部和尾部，只剩1500字节，也就是MTU(最大传输单元)的由来，并约定最小传输长度64字节。

## 2. 服务端
用 `ifonfig` 查看本机的网络设备(网卡):
```sh
eth0: flags=4163<UP,BROADCAST,RUNNING,MULTICAST>  mtu 1500
        inet 172.17.0.2  netmask 255.255.0.0  broadcast 172.17.255.255
        ether 02:42:ac:11:00:02  txqueuelen 0  (Ethernet)
```
通过Go提高的net拿到网络接口设备的详细信息，eth0是上面的网络设备名字：
```go
	ifi, err := net.InterfaceByName("eth0")
    util.CheckError(err)
```
然后使用原始套接字绑定到该网络设备上：
```go
fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(wire.Htons(0x800)))
```
AF_PACKET是Linux 2.2加入的功能，可以在网络设备上接收发送数据包。其第二个参数 SOCK_RAW 表示带有链路层的头部，还有个可选值 SOCK_DGRAM 会移除掉头部。第三个则对应头部中协议类型(ehter type)，比如只接收 IP 协议的数据，也可以接收所有的。可在Linux中[if_ether](https://github.com/spotify/linux/blob/master/include/linux/if_ether.h#L42:9)文件查看相应的值。比如：
```c
#define ETH_P_IP	0x0800		/* Internet Protocol packet	
#define ETH_P_IPV6	0x86DD		/* IPv6 over bluebook		*/
#define ETH_P_SNAP	0x0005		/* Internal only		*/
```
Htons函数是把网络字节序转成当前机器字节序。这里已经拿到链接层socket的连接句柄，下一步就可以监听该句柄的数据：
```go
for {
    buf := make([]byte, 1514)
    n, _, _ := syscall.Recvfrom(fd, buf, 0)
    header := wire.ParseHeader(buf[0:14])
    fmt.Println(header)
}    
```
这时候所有到这机器上的IP协议流量都能监听到，不管UDP，TCP，ICMP等上层协议。启动程序，尝试在另外台机器`ping`下，得到：
```sh
root@4b56d41e5168:/ethernet# go run main.go
[2018-07-16T00:32:32.215Z] INFO 02:42:ac:11:00:02
DestinationAddress: 02:42:ac:11:00:02 SourceAddress: 02:42:ac:11:00:03 EtherType: ipv4
```
另外台机器：
```sh
root@3348477f42e8:/# ping 172.17.0.2
PING 172.17.0.2 (172.17.0.2) 56(84) bytes of data.
64 bytes from 172.17.0.2: icmp_seq=1 ttl=64 time=0.202 ms
```
## 3. 协议头部
上面例子代码中，定义了1514的字节slice来接收一次以太网的数据，然后取出前14个字节来解析头部。协议尾部的4字节不需要处理，在发送数据的时候由网络设备并添加，接收的时候由设备校验并去除。在以前的有些计算机中，是需要自己添加或移除尾部的，后面可介绍下该校验算法。 ParseHeader解析头部也很简单，前6个字节是目标Mac地址，中间6字节是源Mac地址，后2字节是协议类型:
```go
func ParseHeader(buf []byte) *Header {
	header := new(Header)
	var hd net.HardwareAddr
	hd = buf[0:6]
	header.DestinationAddress = hd
	hd = buf[6:12]
	header.SourceAddress = hd
	header.EtherType = binary.BigEndian.Uint16(buf[12:14])
	return header
}
```
ping使用的是ICMP协议，和TCP/UDP同级，所以根据接收到的数据继续解IP协议头部，ICMP协议头部。包含关系如图：
![ICMP-datagram-transmission.jpg](img/ICMP-datagram-transmission.jpg)   

Go官方有相应的库可以解析：
```go
ip4header, _ := ipv4.ParseHeader(buf[14:34])
fmt.Println("ipv4 header: ", ip4header)
icmpPayload := buf[34:]
msg, _ := icmp.ParseMessage(1, icmpPayload)
fmt.Println("icmp: ", msg)
```
IP头部20字节，ICMP头部8个字节，输出如下：
```sh
root@4b56d41e5168://ethernet# go run main.go
[2018-07-16T00:36:03.033Z] INFO 02:42:ac:11:00:02
DestinationAddress: 02:42:ac:11:00:02 SourceAddress: 02:42:ac:11:00:03 EtherType: ipv4
ipv4 header:  ver=4 hdrlen=20 tos=0x0 totallen=84 id=0x97ab flags=0x2 fragoff=0x0 ttl=64 proto=1 cksum=0x4ad6 src=172.17.0.3 dst=172.17.0.2
icmp:  &{echo 0 12964 0xc4200807e0}
```
## 4. 客户端
上面代码是服务端解析以太网协议头部，也可以自定义发送时头部：
建立socket句柄：
```go
var ohter = net.HardwareAddr{0x02, 0x42, 0xac, 0x11, 0x00, 0x02}
var etherType uint16 = 52428
fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(wire.Htons(etherType)))
```
构建以太网头部，然后发送监听的机器上：
```go
for {
		payload := []byte("msg")
		minPayload := len(payload)
		if minPayload < 46 {
			minPayload = 46
		}
		b := make([]byte, 14+minPayload)
		header := &wire.Header{
			DestinationAddress: broadcast,
			SourceAddress:      ifi.HardwareAddr,
			EtherType:          etherType,
		}
		copy(b[0:14], header.Marshal())
		copy(b[14:14+len(payload)], payload)

		var baddr [8]byte
		copy(baddr[:], broadcast)
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
```
监听端输出：
```go
root@4b56d41e5168:/ethernet# go run main.go
[2018-07-16T15:25:46.745Z] INFO 02:42:ac:11:00:02
DestinationAddress: 02:42:ac:11:00:02 SourceAddress: 02:42:ac:11:00:03 EtherType: unknow52428
DestinationAddress: 02:42:ac:11:00:02 SourceAddress: 02:42:ac:11:00:03 EtherType: unknow52428
```
## 5. 总结
基于链接层套接字，就可以抓取数据链路层的流量，对流量进行深入分析等。还有一种方式是基于packet_mmap的共享内存方式，性能更好些。文中例子代码在[examples](https://github.com/mushroomsir/blog/tree/master/examples/002)。参考：
https://github.com/spotify/linux/blob/master/include/linux/if_ether.h
http://man7.org/linux/man-pages/man7/packet.7.html