

* [1. 产生原因](#%E4%BA%A7%E7%94%9F%E5%8E%9F%E5%9B%A0)
* [2. 导致问题](#%E5%AF%BC%E8%87%B4%E9%97%AE%E9%A2%98)
* [3. Nginx](#nginx)
   * [3.1 长连接](#%E9%95%BF%E8%BF%9E%E6%8E%A5)
 * [4. 解决方案](#%E8%A7%A3%E5%86%B3%E6%96%B9%E6%A1%88)
* [5 .参考](#%E5%8F%82%E8%80%83)

## 产生原因

<img src="./img/tcp-close.png" alt="image" style="zoom: 50%;" />

TCP 连接关闭时，会有 4 次通讯（四次挥手），来确认双方都停止收发数据了。如上图，主动关闭方，最后发送 ACK 时，会进入 TIME_WAIT 状态，要等 2MSL 时间后，这条连接才真正消失。

**为什么要进入 TIME_WAIT 状态？**

TCP 的可靠传输机制要求，被动关闭方（简称 S）要确保最后发送的 FIN K 对方能收到。比如网络中的某个路由器出现异常，主动关闭方（简称 C）回复的 ACK K+1 没有及时到达，S 就会重发 FIN K 给 C。如果此时  C 不进入 TIME_WAIT 状态，立马关闭连接，会有 2 种情况：

1. C 机器上，有可能新起的连接会重用旧连接的端口，此时新连接就会收到 S 端重发的 FIN K 消息，导致新连接传输出现错误。
2. C 机器上，并没有用旧连接端口，此时会回复给 S 端一个 RST 类型的消息，应用程序报 connect reset by peer 异常。

为避免上面情况， TCP 会等待 2 MSL 时间，让 S 发的 FIN K 和 C 回复的 ACK K+1 在网络上消失，才真正清除掉连接。

 **为什么等待 2 MSL 时间？**

MSL是 Maximum Segment Lifetime的英文缩写，可译为“最长报文段寿命”，是 TCP 协议规定报文段在网络中最长生存时间，超出后报文段就会被丢弃。RFC793 定义 MSL 为 2 分钟，一般 Linux 会默认设置个更小的值 30 秒。

MSL 时间，是从 C 回复 ACK 后开始 TIME_WAIT 计时，如果这期间收到 S 重发的 FIN 在回复 ACK 后，重新开始计时。这块代码是 Linux tcp_timewait_state_process 函数处理的。

而 2 MSL 是为了确保 C 和 S 两端发送的数据都在网络中消失，不会影响后续的新连接，该如何理解？

假设 C 回复 ACK ，S 经过 t 时间后收到，则有 0 < t <= MSL，因为 C 并不知道 S 多久收到，所以 C 至少要维持 MSL 时间的 TIME_WAIT 状态，才确保回复的 ACK 从网络中消失。 如果 S 在 MSL 时间收到 ACK， 而收到前一瞬间， 因为超时又重传一个 FIN ，这个包又要 MSL 时间才会从网络中消失。

回复需要 MSL 消失 +  发送需要 MSL 消失 = 2 MSL。

## 导致问题

从前面的分析来看，出现 TIME_WAIT 属于正常行为。但在实际生产环境中，大量的 TIME_WAIT 会导致系统异常。

假设前面的 C 是 Client，S 是 Server，如果 C 或 出现大量的 TIME_WAIT，会导致新连接无端口可以用，出现

```Cannot assign requested address``` 错误。这是因为端口被占完了，Linux 一般默认端口范围是：32768-61000，可以通过 ` cat /proc/sys/net/ipv4/ip_local_port_range ` 来查看。根据 TCP 连接四元组计算，C 连接 S 最多有 28232 可以用，也就是说最多同时有 28232 个连接保持。

看着挺多，但如果用短连接的话很快就会出现上面错误，因为每个连接关闭后，需要保持 2 MSL 时间，也就是 4分钟。这意味着 4 分钟内最多建立 28232 个连接，每秒钟 117 个，在高并发系统下一般不够用的。

## Nginx

连接主动关闭方会进入 TIME_WAIT，如果 C 先关闭，C 会出现上面错误。如果是客户端时真正的客户（浏览器），一般不会触发上面的错误。

如果 C 是应用程序或代理，比如 Nginx，此时链路是：浏览器 -> Nginx -> 应用。 因为 Nginx 是转发请求，自身也是客户端，所以如果 Nginx 到应用是短连接，每次转发完请求都主动关闭连接，那很快会触发到端口不够用的错误。

Nginx 默认配置连接到后端是 HTTP/1.0 不支持 HTTP keep-alive，所以每次后端应用都会主动关闭连接，这样后端出现 TIME_WAIT，而 Nginx 不会出现。

后端出现大量的 TIME_WAIT 一般问题不明显，有个需要注意的点是：

查看服务器上`/var/log/messages` 有没有 `TCP: time wait bucket table overflow` 的日志，有的话是超出最大 TIME_WAIT 的数量了，超出后系统会把多余的 TIME_WAIT 删除掉，会导致前面章节介绍的 2 种情况。

这个错误可以调大内核参数 `/etc/sysctl.conf` 中 `tcp_max_tw_buckets` 来解决。

### 长连接

另外个解决方案是 Nginx 与后端调用，启用 HTTP/1.1 开启  keep-alive ，保持长连接。配置如下：

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

## 解决方案

除保持长连接外，调整系统参数也可以解决大量 TIME_WAIT 的问题。

**加快回收**

tcp_tw_timeout = 30：表示连接在 TIME_WAIT 状态下的过期时间。这里配置 30 秒后回收，如前面计算调整后 28232 / 30 = 936， 每秒钟可建立连接 936 个。

**增加端口数量**

ip_local_port_range = 1024  65535:  调整后最大端口数量 64511，64511 / 30 = 2150，每秒钟可建立连接 2150 个。

**复用 TIME_WAIT  连接**

tcp_tw_reuse = 1： 1 表示开启复用 TIME_WAIT  状态的连接，这个参数在 Linux tcp_twsk_unique 函数中读取的。

```c
	int reuse = sock_net(sk)->ipv4.sysctl_tcp_tw_reuse;
	// tcptw->tw_ts_recent_stamp 为 1 表示旧的 TIME_WAIT 连接是携带时间戳的，需要开启 tcp_timestamps (已默认开启)。
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

**其他**

tcp_tw_recycle 也有效果，但不建议调整，Linux 4.12 后已经移除这个参数了，这里不作介绍了。

调整命令：

```shell
// 临时生效
sysctl -w net.ipv4.tcp_tw_reuse = 1
sysctl -p

// 长久生效
vi /etc/sysctl.conf
```

## 参考

http://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_set_header

https://github.com/torvalds/linux

https://www.kernel.org/doc/Documentation/networking/ip-sysctl.txt