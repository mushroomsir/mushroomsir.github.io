

* [产生原因](#%E4%BA%A7%E7%94%9F%E5%8E%9F%E5%9B%A0)
  * [TIME\_WAIT 状态](#time_wait-%E7%8A%B6%E6%80%81)
  * [2 MSL 时间](#2-msl-%E6%97%B6%E9%97%B4)
  * [序列号回绕](#%E5%BA%8F%E5%88%97%E5%8F%B7%E5%9B%9E%E7%BB%95)
* [导致问题](#%E5%AF%BC%E8%87%B4%E9%97%AE%E9%A2%98)
* [Nginx](#nginx)
  * [长连接](#%E9%95%BF%E8%BF%9E%E6%8E%A5)
* [参数优化](#%E5%8F%82%E6%95%B0%E4%BC%98%E5%8C%96)
  * [复用 TIME\_WAIT  连接](#%E5%A4%8D%E7%94%A8-time_wait--%E8%BF%9E%E6%8E%A5)
  * [增加端口数量](#%E5%A2%9E%E5%8A%A0%E7%AB%AF%E5%8F%A3%E6%95%B0%E9%87%8F)
  * [加快回收](#%E5%8A%A0%E5%BF%AB%E5%9B%9E%E6%94%B6)
  * [其他](#%E5%85%B6%E4%BB%96)
* [参考](#%E5%8F%82%E8%80%83)

## 产生原因

![](https://raw.githubusercontent.com/mushroomsir/blog/master/img/tcp-close.png)

TCP 连接关闭时，会有 4 次通讯（四次挥手），来确认双方都停止收发数据了。如上图，主动关闭方，最后发送 ACK 时，会进入 TIME_WAIT 状态，要等 2MSL 时间后，这条连接才真正消失。

### TIME_WAIT 状态

TCP 的可靠传输机制要求，被动关闭方（简称 S）要确保最后发送的 FIN K 对方能收到。比如网络中的某个路由器出现异常，主动关闭方（简称 C）回复的 ACK K+1 没有及时到达，S 就会重发 FIN K 给 C。如果此时  C 不进入 TIME_WAIT 状态，立马关闭连接，会有 2 种情况：

1. C 机器上，有可能新起的连接会重用旧连接的端口，此时新连接就会收到 S 端重发的 FIN K 消息，可能干扰新连接传输数据。
2. C 机器上，并没有用旧连接端口，此时会回复给 S 端一个 RST 类型的消息，应用程序报 connect reset by peer 异常。

为避免上面情况， TCP 会等待 2 MSL 时间，让 S 发的 FIN K 和 C 回复的 ACK K+1 在网络上消失，才真正清除掉连接。

### 2 MSL 时间

MSL是 Maximum Segment Lifetime的英文缩写，可译为“最长报文段寿命”，是 TCP 协议规定报文段在网络中最长生存时间，超出后报文段就会被丢弃。RFC793 定义 MSL 为 2 分钟，Linux 实现会默认设置 30 秒。

MSL 时间，是从 C 回复 ACK 后开始 TIME_WAIT 计时，如果这期间收到 S 重发的 FIN 在回复 ACK 后，重新开始计时。这块代码是 Linux tcp_timewait_state_process 函数处理的。

2 MSL 是为了确保 C 和 S 两端发送的数据都在网络中消失，不会影响后续的新连接，该如何理解？

假设 C 回复 ACK ，S 经过 t 时间后收到，则有 0 < t <= MSL，因为 C 并不知道 S 多久收到，所以 C 至少要维持 MSL 时间的 TIME_WAIT 状态，才确保回复的 ACK 从网络中消失。 如果 S 在 MSL 时间收到 ACK， 而收到前一瞬间， 因为超时又重传一个 FIN ，这个包又要 MSL 时间才会从网络中消失。

`回复` MSL 后消失 +  `发送` MSL 后消失 = 2 MSL。

### 序列号回绕

前面介绍的第一种情况，可能会干扰新连接数据的原因，在于 TCP 传输数据数据时会携带 sequence number。这个值每次传输时会加上要传输的字节数量，单位是无符号 32 位的，最大 2^32 - 1，双方交换大约 4G 数据，就会回绕到 0 重新计算。注意初始值 ISN 并不是 0 ，而是随机的。

假设立马关闭 TIME_WAIT 连接并复用，这条新连接，在协议规定的 2 分钟 MSL 内，就发生回绕。在低速互联网时代，没有这样的问题，传输 4G 数据，早超过旧连接数据段的最大 MSL 了。 带宽回绕临界值如下：

```
 网络          bits/sec     bytes/sec  回绕时间（秒）
 ARPANET       56kbps       7KBps    3*10**5 (~3.6 days)
 DS1          1.5Mbps     190KBps    10**4 (~3 hours)
 Ethernet      10Mbps    1.25MBps    1700 (~30 mins)
 DS3           45Mbps     5.6MBps    380
 FDDI         100Mbps    12.5MBps    170
```

这个回绕介绍在 rfc1185 有更详细的介绍。解决办法就是增加时间戳（tcp_timestamps），用以区分是新序列号，还是回绕后的重复序列号。

## 导致问题

从前面的分析来看，出现 TIME_WAIT 属于正常行为。但在实际生产环境中，大量的 TIME_WAIT 会导致系统异常。

假设前面的 C 是 Client，S 是 Server，如果 C 出现大量的 TIME_WAIT，会导致新连接无端口可以用，出现

```Cannot assign requested address``` 错误。这是因为端口被占完了，Linux 一般默认端口范围是：32768-61000，可以通过 ` cat /proc/sys/net/ipv4/ip_local_port_range ` 来查看。根据 TCP 连接四元组计算，C 连接 S 最多有 28232 可以用，也就是说最多同时有 28232 个连接保持。

看着挺多，但如果用短连接的话很快就会出现上面错误，因为每个连接关闭后，需要保持 2 MSL 时间，也就是 1分钟。这意味着 1 分钟内最多建立 28232 个连接，每秒钟 470 个，在高并发系统下肯定是不够用的。

## Nginx

连接主动关闭方会进入 TIME_WAIT，如果 C 先关闭，C 会出现上面错误。如果是客户端时真正的客户（浏览器），一般不会触发上面的错误。

如果 C 是应用程序或代理，比如 Nginx，此时链路是：浏览器 -> Nginx -> 应用。 因为 Nginx 是转发请求，自身也是客户端，所以如果 Nginx 到应用是短连接，每次转发完请求都主动关闭连接，那很快会触发到端口不够用的错误。

Nginx 默认配置连接到后端是 HTTP/1.0 不支持 HTTP keep-alive，所以每次后端应用都会主动关闭连接，这样后端出现 TIME_WAIT，而 Nginx 不会出现。

后端出现大量的 TIME_WAIT 一般问题不明显，有个需要注意的点是：

查看服务器上`/var/log/messages` 有没有 `TCP: time wait bucket table overflow` 的日志，有的话是超出最大 TIME_WAIT 的数量了，超出后系统会把多余的 TIME_WAIT 删除掉，会导致前面章节介绍的 2 种情况。

这个错误可以调大内核参数 `/etc/sysctl.conf` 中 `tcp_max_tw_buckets` 来解决。

### 长连接

一个解决方案是 Nginx 与后端调用，启用 HTTP/1.1 开启  keep-alive ，保持长连接。配置如下：

```nginx
http{
    upstream www{
        keepalive 500;  # 保持和后端的最大空闲连接数量
    }
    proxy_set_header X-Real-IP $remote_addr; ## 不会生效
    server {
        location / {
        proxy_http_version 1.1;  # 启用 HTTP/1.1
        proxy_set_header Connection "";   
        }
    }
}
```

 `proxy_set_header Connection "";  ` 这个配置是设置 Nginx 请求后端的 Connection header 的值为空。目的是防止客户端传值 `close` 给 Nginx，Nginx 又转发给后端，导致无法保持长连接。

在 Nginx 配置中有个注意的点是：当前配置 location 中如果定义了 proxy_set_header ，则不会从上级继承`proxy_set_header `了，如上面配置的 `proxy_set_header X-Real-IP $remote_addr` 则不会生效。

没有显示定义的 header，Nginx 默认只带下面 2 个 header：

```
proxy_set_header Host $proxy_host;
proxy_set_header Connection close; 
```

## 参数优化

除保持长连接外，调整系统参数也可以解决大量 TIME_WAIT 的问题。

### 复用 TIME_WAIT  连接

设置 tcp_tw_reuse = 1： 1 表示开启复用 TIME_WAIT  状态的连接，复用的前提条件：

1. 要同时开启 tcp_timestamps ，已默认开启；
2. 旧连接最后收到数据段超过 1 秒；

 这 2 个条件保证从数据完整性的角度，复用是安全的。为什么这么说呢？

前面介绍快速关闭并复用，会导致旧连接的数据段发给新连接。开启复用后 TCP 如果收到旧连接的数据段，发现时间小于新连接的接收时间，会直接丢弃掉，这样就不会干扰新连接数据。

这个参数在 Linux tcp_twsk_unique 函数中读取的：

```c
	int reuse = sock_net(sk)->ipv4.sysctl_tcp_tw_reuse;
	// tcptw->tw_ts_recent_stamp 为 1 表示旧的 TIME_WAIT 连接是携带时间戳的。
    // tcp_tw_reuse  reuse 开启复用
   // time_after32 表示旧的 TIME_WAIT 连接，最后收到数据已超过 1 秒。
	if (tcptw->tw_ts_recent_stamp &&
	    (!twp || (reuse && time_after32(ktime_get_seconds(),
					    tcptw->tw_ts_recent_stamp)))) {
		if (likely(!tp->repair)) {
			u32 seq = tcptw->tw_snd_nxt + 65535 + 2;

			if (!seq)
				seq = 1;
			WRITE_ONCE(tp->write_seq, seq);
			tp->rx_opt.ts_recent	   = tcptw->tw_ts_recent;
			tp->rx_opt.ts_recent_stamp = tcptw->tw_ts_recent_stamp;
		}
		sock_hold(sktw);
		return 1;
	}
```


### 增加端口数量

ip_local_port_range = 1024  65535:  调整后最大端口数量 64511， 64511 / 60 = 1075，每秒钟可建立连接 1 075 个。

TCP 建立连接选取端口的规则：

1. 检查是否已绑定端口，没有则自动挑选一个；
2. 获取端口范围 inet_get_local_port_range ，计算端口起始值；
3. 从小到大循环，检查是否时保留端口、是否可以复用（上面 tcp_tw_reuse 介绍）

挑选端口的源码如下：

```c
int inet_hash_connect(struct inet_timewait_death_row *death_row,
		      struct sock *sk)
{
	u32 port_offset = 0;

	if (!inet_sk(sk)->inet_num) //检查是否已绑定端口
		port_offset = inet_sk_port_offset(sk);// 计算偏移量
	return __inet_hash_connect(death_row, sk, port_offset,
				   __inet_check_established); // 连接
}
int __inet_hash_connect()
{
    inet_get_local_port_range(net, &low, &high); // 获取端口范围
	high++; /* [32768, 60999] -> [32768, 61000[ */
	remaining = high - low;
	if (likely(remaining > 1))
		remaining &= ~1U;

	offset = (hint + port_offset) % remaining; // 计算偏移量
    for (i = 0; i < remaining; i += 2, port += 2) {
		if (unlikely(port >= high))
			port -= remaining;
		if (inet_is_local_reserved_port(net, port)) // 检查是否保留端口
			continue;
        head = &hinfo->bhash[inet_bhashfn(net, port,
						  hinfo->bhash_size)];  // 找到端口下的连接桶
        inet_bind_bucket_for_each(tb, &head->chain) { //遍历
			if (net_eq(ib_net(tb), net) && tb->l3mdev == l3mdev &&
			    tb->port == port) {  // 已被占用
				if (!check_established(death_row, sk,
						       port, &tw)) // 端口是否可以复用
					goto ok; //成功
				goto next_port; //失败，继续
			}
		}
    }    
}
// check_established -> twsk_unique(前面章节的 tcp_twsk_unique) 
```
### 加快回收

配置连接在 TIME_WAIT 状态下的过期时间。比如设置 10 秒后回收，接着前面计算 64511 / 60 = 6451， 每秒钟可建立连接 6451 个。

修改 TIME_WAIT 过期时间与 TCP/IP 协议相违背，所以在 Llinux 下并没有这个参数，需要修改内核参数编译：

```c
// linux/include/net/tcp.h
#define TCP_TIMEWAIT_LEN (60*HZ) /* how long to wait to destroy TIME-WAIT
				  * state, about 60 seconds	*/
```

也有定制版 Linux 支持修改，比如 Aliyun Linux 2 增加了 tcp_tw_timeout 参数，允许修改过期时间。

详见： [修改TCP TIME-WAIT超时时间](https://help.aliyun.com/document_detail/155470.html)

### 其他

**tcp_tw_recycle** 也有效果，但不建议调整，Linux 4.12 后已经移除这个参数了，这里不做介绍了。

调整参数的命令：

```shell
// 临时生效
sysctl -w net.ipv4.tcp_tw_reuse = 1
sysctl -p

// 长久生效
vi /etc/sysctl.conf
sysctl -p
```

## 参考

https://www.rfc-editor.org/rfc/rfc1185.txt

http://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_set_header

https://github.com/torvalds/linux

https://www.kernel.org/doc/Documentation/networking/ip-sysctl.txt