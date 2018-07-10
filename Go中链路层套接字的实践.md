

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
AF_PACKET是Linux 2.2加入的功能，可以在网络设备上接收发送数据包。其第二个参数 SOCK_RAW 表示带有链路层的头部，另外个可选值 SOCK_DGRAM 会移除掉。第三个则对应头部中协议类型(ehter type)，比如只接收 IP 协议的数据，也可以接收所有的。可在Linux中[if_ether](https://github.com/spotify/linux/blob/master/include/linux/if_ether.h#L42:9)文件查看相应的值。比如：
```c

```