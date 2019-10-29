
- [背景](#背景)
- [排查](#排查)
    - [推测](#推测)
    - [连接超时](#连接超时)
    - [疑问](#疑问)
    - [http2](#http2)
    - [解决超时](#解决超时)
    - [并发连接数](#并发连接数)
    - [服务端限制](#服务端限制)
- [真相](#真相)
    - [重试](#重试)
    - [解决办法](#解决办法)
    - [问题1](#问题1)

## 背景

最新有同事反馈，服务间有调用超时的现象，在业务高峰期发生的概率和次数比较高。从日志中调用关系来看，有2个调用链经常发生超时问题。

问题1： A服务使用 http1.1 发送请求到 B 服务超时。

问题2:  A服务使用一个轻量级 http-sdk(内部http2.0) 发送请求到 C 服务超时。

Golang 给出的报错信息时：

```none
Post http://host/v1/xxxx: net/http: request canceled (Client.Timeout exceeded while awaiting headers)
```

通知日志追踪ID来排查，发现有的请求还没到服务方就已经超时。

有些已经到服务方了，但也超时。

这里先排查的是问题2，下面是过程。

## 排查

### 推测

调用方设置的http请求超时时间是1s。

请求已经到服务端了还超时的原因，可能是：

1. 服务方响应慢。 通过日志排查确实有部分存在。

1. 客户端调用花了990ms，到服务端只剩10ms，这个肯定会超时。

请求没到服务端超时的原因，可能是：

1. golang CPU调度不过来。通过cpu监控排除这个可能性

1. golang 网络库原因。重点排查

排查方法：

本地写个测试程序，1000并发调用测试环境的C服务:

```go
n := 1000
var waitGroutp = sync.WaitGroup{}
waitGroutp.Add(n)
for i := 0; i < n; i++ {
       go func(x int) {
         httpSDK.Request()
     }
}
waitGroutp.Wait()
```

报错：

```go
too many open files    // 这个错误是笔者本机ulimit太小的原因，可忽略
net/http: request canceled (Client.Timeout exceeded while awaiting headers)
```

并发数量调整到500继续测试，还是报同样的错误。

### 连接超时

本地如果能重现的问题，一般来说比较好查些。

开始跟golang的源码，下面是创建httpClient的代码，这个httpClient是全局复用的。

```go
func createHttpClient(host string, tlsArg *TLSConfig) (*http.Client, error) {
    httpClient := &http.Client{
        Timeout: time.Second,
    }
    tlsConfig := &tls.Config{InsecureSkipVerify: true}
    transport := &http.Transport{
        TLSClientConfig:     tlsConfig,
        MaxIdleConnsPerHost: 20,
    }
    http2.ConfigureTransport(transport)
    return httpClient, nil
}
// 使用httpClient
httpClient.Do(req)
```

跳到net/http/client.go 的do方法

```go
func (c *Client) do(req *Request) (retres *Response, reterr error) {
    if resp, didTimeout, err = c.send(req, deadline); err != nil {
    }
}
```

继续进 send 方法，实际发送请求是通过 RoundTrip 函数。

```go
func send(ireq *Request, rt RoundTripper, deadline time.Time) (resp *Response, didTimeout func() bool, err error) {
     rt.RoundTrip(req) 
}
```

send 函数接收的 rt 参数是个 inteface，所以要从 http.Transport 进到 RoundTrip 函数。

其中`log.Println("getConn time", time.Now().Sub(start), x)` 是笔者添加的日志，为了验证创建连接耗时。

```go
var n int
// roundTrip implements a RoundTripper over HTTP.
func (t *Transport) roundTrip(req *Request) (*Response, error) {
    // 检查是否有注册http2，有的话直接使用http2的RoundTrip
    if t.useRegisteredProtocol(req) {
        altProto, _ := t.altProto.Load().(map[string]RoundTripper)
        if altRT := altProto[scheme]; altRT != nil {
            resp, err := altRT.RoundTrip(req)
            if err != ErrSkipAltProtocol {
                return resp, err
            }
        }
    }
    for {
        //n++
        // start := time.Now()
        pconn, err := t.getConn(treq, cm)
         // log.Println("getConn time", time.Now().Sub(start), x)
        if err != nil {
            t.setReqCanceler(req, nil)
            req.closeBody()
            return nil, err
        }
    }
}
```

结论：加了日志跑下来，确实有大量的`getConn time`超时。

### 疑问

这里有2个疑问：

1. 为什么Http2没复用连接，反而会创建大量连接？

1. 创建连接为什么会越来越慢？

继续跟 getConn 源码, getConn第一步会先获取空闲连接，因为这里用的是http2，可以不用管它。

追加耗时日志，确认是dialConn耗时的。

```go
func (t *Transport) getConn(treq *transportRequest, cm connectMethod) (*persistConn, error) {
   if pc, idleSince := t.getIdleConn(cm); pc != nil {
   }
    //n++
    go func(x int) {
        // start := time.Now()
        // defer func(x int) {
        //  log.Println("getConn dialConn time", time.Now().Sub(start), x)
        // }(n)
        pc, err := t.dialConn(ctx, cm)
        dialc <- dialRes{pc, err}
    }(n)
}
```

继续跟dialConn函数，里面有2个比较耗时的地方：

1. 连接建立，三次握手。

1. tls握手的耗时，见下面http2章节的dialConn源码。

分别在dialConn函数中 t.dial 和 addTLS 的位置追加日志。

可以看到，三次握手的连接还是比较稳定的，后面连接的在tls握手耗时上面，耗费将近1s。

```none
2019/10/23 14:51:41 DialTime 39.511194ms https.Handshake 1.059698795s
2019/10/23 14:51:41 DialTime 23.270069ms https.Handshake 1.064738698s
2019/10/23 14:51:41 DialTime 24.854861ms https.Handshake 1.0405369s
2019/10/23 14:51:41 DialTime 31.345886ms https.Handshake 1.076014428s
2019/10/23 14:51:41 DialTime 26.767644ms https.Handshake 1.084155891s
2019/10/23 14:51:41 DialTime 22.176858ms https.Handshake 1.064704515s
2019/10/23 14:51:41 DialTime 26.871087ms https.Handshake 1.084666172s
2019/10/23 14:51:41 DialTime 33.718771ms https.Handshake 1.084348815s
2019/10/23 14:51:41 DialTime 20.648895ms https.Handshake 1.094335678s
2019/10/23 14:51:41 DialTime 24.388066ms https.Handshake 1.084797011s
2019/10/23 14:51:41 DialTime 34.142535ms https.Handshake 1.092597021s
2019/10/23 14:51:41 DialTime 24.737611ms https.Handshake 1.187676462s
2019/10/23 14:51:41 DialTime 24.753335ms https.Handshake 1.161623397s
2019/10/23 14:51:41 DialTime 26.290747ms https.Handshake 1.173780655s
2019/10/23 14:51:41 DialTime 28.865961ms https.Handshake 1.178235202s
```

结论：第二个疑问的答案就是tls握手耗时

### http2

为什么Http2没复用连接，反而会创建大量连接？

前面创建http.Client 时，是通过http2.ConfigureTransport(transport) 方法，其内部调用了configureTransport：

```go
func configureTransport(t1 *http.Transport) (*Transport, error) {
    // 声明一个连接池
   // noDialClientConnPool 这里很关键，指明连接不需要dial出来的，而是由http1连接升级而来的 
    connPool := new(clientConnPool)
    t2 := &Transport{
        ConnPool: noDialClientConnPool{connPool},
        t1:       t1,
    }
    connPool.t = t2
// 把http2的RoundTripp的方法注册到，http1上transport的altProto变量上。
// 当请求使用http1的roundTrip方法时，检查altProto是否有注册的http2，有的话，则使用
// 前面代码的useRegisteredProtocol就是检测方法
    if err := registerHTTPSProtocol(t1, noDialH2RoundTripper{t2}); err != nil           {
        return nil, err
    }
   // http1.1 升级到http2的后的回调函数，会把连接通过 addConnIfNeeded 函数把连接添加到http2的连接池中
    upgradeFn := func(authority string, c *tls.Conn) http.RoundTripper {
        addr := authorityAddr("https", authority)
        if used, err := connPool.addConnIfNeeded(addr, t2, c); err != nil {
            go c.Close()
            return erringRoundTripper{err}
        } else if !used {
            go c.Close()
        }
        return t2
    }
    if m := t1.TLSNextProto; len(m) == 0 {
        t1.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{
            "h2": upgradeFn,
        }
    } else {
        m["h2"] = upgradeFn
    }
    return t2, nil
}
```

TLSNextProto 在 http.Transport-> dialConn 中使用。调用upgradeFn函数，返回http2的RoundTripper，赋值给alt。

alt会在http.Transport 中  RoundTripper 内部检查调用。

```go
func (t *Transport) dialConn(ctx context.Context, cm connectMethod) (*persistConn, error) {
    pconn := &persistConn{
        t:             t,
    }
    if cm.scheme() == "https" && t.DialTLS != nil {
     // 没有自定义DialTLS方法，不会走到这一步
    } else {
        conn, err := t.dial(ctx, "tcp", cm.addr())
        if err != nil {
            return nil, wrapErr(err)
        }
        pconn.conn = conn
        if cm.scheme() == "https" {
         // addTLS 里进行 tls 握手，也是建立新连接最耗时的地方。
            if err = pconn.addTLS(firstTLSHost, trace); err != nil {
                return nil, wrapErr(err)
            }
        }
    }
    if s := pconn.tlsState; s != nil && s.NegotiatedProtocolIsMutual && s.NegotiatedProtocol != "" {
		if next, ok := t.TLSNextProto[s.NegotiatedProtocol]; ok {
            // next 调用注册的升级函数
			return &persistConn{t: t, cacheKey: pconn.cacheKey, alt: next(cm.targetAddr, pconn.conn.(*tls.Conn))}, nil
		}
	}
    return pconn, nil
}

```

结论：

当没有连接时，如果此时来一大波请求，会创建n多http1.1的连接，进行升级和握手，而tls握手随着连接增加而变的非常慢。

### 解决超时

上面的结论并不能完整解释，复用连接的问题。因为服务正常运行的时候，一直都有请求的，连接是不会断开的，所以除了第一次连接或网络原因断开，正常情况下都应该复用http2连接。

通过下面测试，可以复现有http2的连接时，还是会创建N多新连接：

```go
sdk.Request()  // 先请求一次，建立好连接，测试是否一直复用连接。
time.Sleep(time.Second)
n := 1000
var waitGroutp = sync.WaitGroup{}
waitGroutp.Add(n)
for i := 0; i < n; i++ {
       go func(x int) {
         sdk.Request()
     }
}
waitGroutp.Wait()
```

所以还是怀疑http1.1升级导致，这次直接改成使用 http2.Transport

```go
httpClient.Transport = &http2.Transport{
            TLSClientConfig: tlsConfig,
}
```

改了后，测试发现没有报错了。

为了验证升级模式和直接http2模式的区别。 这里先回到升级模式中的 addConnIfNeeded 函数中，其会调用addConnCall 的 run 函数：

```go
func (c *addConnCall) run(t *Transport, key string, tc *tls.Conn) {
    cc, err := t.NewClientConn(tc)
}
```

run参数中传入的是http2的transport。

整个解释是http1.1创建连接后，会把传输层连接，通过addConnIfNeeded->run->Transport.NewClientConn构成一个http2连接。  因为http2和http1.1本质都是应用层协议，传输层的连接都是一样的。

然后在newClientConn连接中加日志。

```go
func (t *Transport) newClientConn(c net.Conn, singleUse bool) (*ClientConn, error) {
    //  log.Println("http2.newClientConn")
}
```

结论：

升级模式下，会打印很多http2.newClientConn，根据前面的排查这是讲的通的。而单纯http2模式下，也会创建新连接，虽然很少。

### 并发连接数

那http2模式下什么情况下会创建新连接呢？

这里看什么情况下http2会调用 newClientConn。回到clientConnPool中，dialOnMiss在http2模式下为true，getStartDialLocked 里会调用dial->dialClientConn->newClientConn。

```go
func (p *clientConnPool) getClientConn(req *http.Request, addr string, dialOnMiss bool) (*ClientConn, error) {
    p.mu.Lock()
    for _, cc := range p.conns[addr] {
        if st := cc.idleState(); st.canTakeNewRequest {
            if p.shouldTraceGetConn(st) {
                traceGetConn(req, addr)
            }
            p.mu.Unlock()
            return cc, nil
        }
    }
    if !dialOnMiss {
        p.mu.Unlock()
        return nil, ErrNoCachedConn
    }
    traceGetConn(req, addr)
    call := p.getStartDialLocked(addr)
    p.mu.Unlock()
  }

```

 有连接的情况下，canTakeNewRequest 为false，也会创建新连接。看看这个变量是这么得来的：

```go
func (cc *ClientConn) idleStateLocked() (st clientConnIdleState) {
    if cc.singleUse && cc.nextStreamID > 1 {
        return
    }
    var maxConcurrentOkay bool
    if cc.t.StrictMaxConcurrentStreams {
        maxConcurrentOkay = true
    } else {
        maxConcurrentOkay = int64(len(cc.streams)+1) < int64(cc.maxConcurrentStreams)
    }
    st.canTakeNewRequest = cc.goAway == nil && !cc.closed && !cc.closing && maxConcurrentOkay &&
        int64(cc.nextStreamID)+2*int64(cc.pendingRequests) < math.MaxInt32
    // if st.canTakeNewRequest == false {
    //  log.Println("clientConnPool", cc.maxConcurrentStreams, cc.goAway == nil, !cc.closed, !cc.closing, maxConcurrentOkay, int64(cc.nextStreamID)+2*int64(cc.pendingRequests) < math.MaxInt32)
    // }
    st.freshConn = cc.nextStreamID == 1 && st.canTakeNewRequest
    return
}
```

为了查问题，这里加了详细日志。测试下来，发现是maxConcurrentStreams 超了，canTakeNewRequest才为false。

在http2中newClientConn的初始化配置中, maxConcurrentStreams 默认为1000：

```go
   maxConcurrentStreams:  1000,     // "infinite", per spec. 1000 seems good enough.
```

但实际测下来，发现500并发也会创建新连接。继续追查有设置这个变量的地方：

```go
func (rl *clientConnReadLoop) processSettings(f *SettingsFrame) error {
    case SettingMaxConcurrentStreams:
            cc.maxConcurrentStreams = s.Val
           //log.Println("maxConcurrentStreams", s.Val)
}

```

运行测试，发现是服务传过来的配置，值是250。

结论： 服务端限制了单连接并发连接数，超了后就会创建新连接。

### 服务端限制

在服务端框架中，找到ListenAndServeTLS函数，跟下去->ServeTLS->Serve->setupHTTP2_Serve->onceSetNextProtoDefaults_Serve->onceSetNextProtoDefaults->http2ConfigureServer。

查到new(http2Server)的声明，因为web框架即支持http1.1 也支持http2，所以没有指定任何http2的相关配置，都使用的是默认的。

```go
// Server is an HTTP/2 server.
type http2Server struct {
    // MaxConcurrentStreams optionally specifies the number of
    // concurrent streams that each client may have open at a
    // time. This is unrelated to the number of http.Handler goroutines
    // which may be active globally, which is MaxHandlers.
    // If zero, MaxConcurrentStreams defaults to at least 100, per
    // the HTTP/2 spec's recommendations.
    MaxConcurrentStreams uint32
}   
```

从该字段的注释中看出，http2标准推荐至少为100，golang中使用默认变量 http2defaultMaxStreams， 它的值为250。

## 真相

上面的步骤，更多的是为了记录排查过程和源码中的关键点，方便以后类似问题有个参考。

简化来说：

1. 调用方和服务方使用http1.1升级到http2的模式进行通讯
2. 服务方http2Server限制单连接并发数是250
3. 当并发超过250，比如1000时，调用方就会并发创建750个连接。这些连接的tls握手时间会越来越长。而调用超时只有1s，所以导致大量超时。
4. 这些连接有些没到服务方就超时，有些到了但服务方还没来得及处理，调用方就取消连接了，也是超时。

并发量高的情况下，如果有网络断开，也会导致这种情况发送。

### 重试

A服务使用的轻量级http-sdk有一个重试机制，当检测到是一个临时错误时，会重试2次。

```go
Temporary() bool // Is the error temporary?
```

而这个超时错误，就属于临时错误，从而放大了这种情况发生。

### 解决办法

不是升级模式的http2即可。

```go
httpClient.Transport = &http2.Transport{
            TLSClientConfig: tlsConfig,
}
```

为什么http2不会大量创建连接呢？

这是因为http2创建新连接时会加锁，后面的请求解锁后，发现有连接没超过并发数时，直接复用连接即可。所以没有这种情况，这个锁在 clientConnPool.getStartDialLocked 源码中。

### 问题1

问题1： A服务使用 http1.1 发送请求到 B 服务超时。 

问题1和问题2的原因一样，就是高并发来的情况下，会创建大量连接，连接的创建会越来越慢，从而超时。

这种情况没有很好的办法解决，推荐使用http2。

如果不能使用http2，调大MaxIdleConnsPerHost参数，可以缓解这种情况。默认http1.1给每个host只保留2个空闲连接，来个1000并发，就要创建998新连接。

该调整多少，可以视系统情况调整，比如50，100。

