---
title: 'GeeRPC 笔记总结'
date: 2023-10-13
category:
 - golang
---

RPC(Remote Procedure Call，远程过程调用)是一种计算机通信协议，允许调用不同进程空间的程序。RPC 的客户端和服务器可以在一台机器上，也可以在不同的机器上。使用时，就像调用本地程序一样，无需关注内部的实现细节。

## 1. 消息（报文）的序列化和反序列化

RPC 客户端和服务端通信报文可以划分为两个部分：

1. 报文头(Header)：
   包含调用的服务名，请求序列号和请求的错误信息

   ```go
   type Header struct {
   	ServiceMethod string // format "Service.Method"
   	Seq           uint64 // sequence number chosen by client
   	Error         string
   }
   ```

2. 报文体(Body)：请求服务的参数

不同的报文格式所需的编解码方式不同，可以抽象出编解码的接口，以支持不同的报文格式：

```go
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(any) error
	Write(*Header, any) error
}
```

- `io.Closer`：需要实现其`Close() error`方法
- `ReadHeader`：读取报文头
- `ReadBody`：读取报文体
- `Write`：向客户端发送完整的响应报文（Header+Body）

### 1.1 使用 gob 

```go
type GobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}
```

`GobCodec`：

1. `conn`：连接实例
2. `dec`：用于解码接收到的报文
3. `buf`：带缓冲的 writer，避免阻塞以提升性能
4. `enc`：用于编码发送的报文

### 1.2 通信协商

客户端在发送请求之前需要告知服务端请求相关的信息：

```go
type Option struct {
	MagicNumber    int           // MagicNumber marks this is a geerpc request
	CodecType      codec.Type    // CodecType
	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration
}
```

- `MagicNumber`：标识报文为 RPC 报文
- `CodeType`：编码方式
- `ConnectTimtout`：连接超时时间
- `HandleTimeout`：请求处理超时时间

一般 Option 使用固定字节编码，为了实现方便此处使用 JSON。

![image-20231014163522531](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310141635602.png)

## 2. 设计

### 2.1 服务 service

在 `net/rpc`中函数能够被远程调用，需要满足五个条件：

1. he method’s type is exported. – 方法所属类型是导出的。
2. the method is exported. – 方式是导出的。
3. the method has two arguments, both exported (or builtin) types. – 两个入参，均为导出或内置类型。
4. the method’s second argument is a pointer. – 第二个入参必须是一个指针。
5. the method has return type error. – 返回值为 error 类型

```go
func (t *T) MethodName(argType T1, replyType *T2) error
```

因为调用的服务是动态的，所以需要通过**反射**将结构体映射为服务。

```go
type service struct {
    name   string
    typ    reflect.Type
    rcvr   reflect.Value
    method map[string]*methodType
}
```

- `name`：服务名，即结构体名
- `typ`：结构体类型
- `rcvr`：结构体实例
- `method`：方法名对应的方法类型

```go
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnVals := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnVals[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
```

- `registerMethods`：通过反射添加满足条件的方法
- `call`：调用指定的方法

### 2.2 服务端 server

```go
// Server represents an RPC server
type Server struct {
    serviceMap sync.Map
}
```

- `serviceMap`：服务名对应的`Service`实例，使用 `sync.Map` 保证并发安全

#### 服务注册

```go
// Register publishes in the server the set of methods of the
func (server *Server) Register(rcvr any) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined:" + s.name)
	}
	return nil
}
```

1. 通过传入的对象，构建`service`实例
2. 添加至服务映射表中

#### 请求处理

```go
// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go server.ServeConn(conn)
	}
}
```

在无限循环中等待连接建立，并开启新的协程进行处理。

```go
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type: %s", opt.CodecType)
		return
	}
	server.serveCodec(f(conn), &opt)
}
```

1. 读取客户端发送的 Option 信息
2. 选择编码方式
3. 开始处理请求

```go
func (server *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex) // make sure to send a complete response
	wg := new(sync.WaitGroup)  // wait until all request are handled
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}
```

1. 在无限循环中持续处理请求，直到出现错误 或 连接关闭
2. 开启新的协程处理请求

```go
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})

	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: exepct within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}
```

1. 开启新协程进行函数调用
2. 等待调用并发送响应完成或超时

```go
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body any, sending *sync.Mutex) {
    sending.Lock()
    defer sending.Unlock()
    if err := cc.Write(h, body); err != nil {
       log.Println("rpc server: write response error:", err)
    }
}
```

- `sending`：互斥锁保证响应报文的完整性

#### 服务端处理流程

![image-20231015091631838](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310150916772.png)

### 2.3 RPC 调用 Call

```go
type Call struct {
	Seq           uint64
	ServiceMethod string     // format "<service>.<method>"
	Args          any        // arguments to the func
	Reply         any        // reply from the func
	Error         error      // if error occurs, it will be set
	Done          chan *Call // Strobes when call is complete.
}
```

`Call`用于保存一次 RPC 调用的相关信息：

1. `Seq`：请求序列号
2. `ServiceMethod`：服务及方法
3. `Args`：请求参数
4. `Reply`：请求返回值
5. `Error`：请求错误
6. `Done`：channel，存储 Call

### 2.4 客户端 Client

```go
type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex // protect following
	header   codec.Header
	mu       sync.Mutex // protect following
	seq      uint64
	pending  map[uint64]*Call
	closing  bool // user has called Close
	shutdown bool // server told to stop
}
```

- `cc`：编解码器
- `opt`：通信协商数据
- `sending`：互斥锁，保证请求报文的完整性
- `header`：报文头
- `mu`：互斥锁，保证以下数据的并发安全
- `seq`：报文序列号
- `pending`：待处理的 Call
- `closing`：标识 Client 关闭，由客户端发起
- `shutdown`：标识 Client 关闭，由服务端发起 或 出现错误

```go
func (call *Call) done() {
	call.Done <- call
}
```

将当前 Call 实例发送至 Channel，通知调用方处理结果。

#### 建立客户端连接

```go
type clientResult struct {
	client *Client
	err    error
}

// Dial connects to an RPC server at the specified network address
func Dial(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewClient, network, address, opts...)
}

func dialTimeout(f newClientFunc, network, address string, opts ...*Option) (*Client, error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()

	if opt.ConnectTimeout == 0 {
		res := <-ch
		return res.client, res.err
	}

	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case res := <-ch:
		return res.client, res.err
	}
}
```

1. 调用`net.DialTimeout`建立连接
2. 启用子协程创建客户端实例
3. 等待客户端创建成功或超时

```go
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
    f := codec.NewCodecFuncMap[opt.CodecType]
    if f == nil {
       err := fmt.Errorf("invalid codec type %s", opt.CodecType)
       log.Println("rpc client: codec error:", err)
       return nil, err
    }
    // send option to server
    if err := json.NewEncoder(conn).Encode(opt); err != nil {
       log.Println("rpc client: options error:", err)
       _ = conn.Close()
       return nil, err
    }
    return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
    client := &Client{
       seq:     1, // seq start with 1, 0 means invalid call
       cc:      cc,
       opt:     opt,
       pending: make(map[uint64]*Call),
    }
    go client.receive()
    return client
}
```

1. 通过 Option 选择编解码器
2. 发送 Option 报文
3. 创建 Client 实例
4. 启用子协程接收响应报文

#### 接收响应报文

```go
func (client *Client) receive() {
    var err error
    for err == nil {
       var h codec.Header
       if err = client.cc.ReadHeader(&h); err != nil {
          break
       }

       call := client.removeCall(h.Seq)
       switch {
       case call == nil:
          // write partially failed or call already removed
          err = client.cc.ReadBody(nil)
       case h.Error != "":
          call.Error = fmt.Errorf(h.Error)
          err = client.cc.ReadBody(nil)
          call.done()
       default:
          err = client.cc.ReadBody(call.Reply)
          if err != nil {
             call.Error = errors.New("reading body " + err.Error())
          }
          call.done()
       }
    }
    // error occurs, terminates all pending calls
    client.terminateCalls(err)
}
```

1. 无限循环中，持续接收响应报文

2. 读取报文头，获取序列号

3. 从等待处理 Call 映射表中删除对应的 Call，表示当前 Call 已经被服务端处理：

   - 若 Call 已不在映射表中，表示 Call 发送失败了 或 已经移除
   - 若 报文头 中 Error 不为空，表示调用失败
   - 否则，读取响应报文体

   调用 `call.done()`返回调用结果

4. 出现通信错误（如连接关闭），终止当前等待处理的 Call

```go
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}
```

`terminateCalls`：将错误通知给调用方

`removeCall`：移除 Call 并返回

#### 发送请求报文

```go
func (client *Client) Go(serviceMethod string, args, reply any, done chan *Call) *Call {
    if done == nil {
       done = make(chan *Call, 10)
    } else if cap(done) == 0 {
       log.Panic("rpc client: done channel is unbuffered")
    }

    call := &Call{
       ServiceMethod: serviceMethod,
       Args:          args,
       Reply:         reply,
       Done:          done,
    }
    client.send(call)
    return call
}

func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply any) error {
    call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
    select {
    case <-ctx.Done():
       client.removeCall(call.Seq)
       return errors.New("rpc client: call failed:" + ctx.Err().Error())
    case c := <-call.Done:
       return c.Error
    }
}
```

- `Go`：异步接口
- `Call`：同步接口，会等待请求返回

```go
func (client *Client) send(call *Call) {
	// make sure that client will send complete request
	client.sending.Lock()
	defer client.sending.Unlock()

	// register call
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// request header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	// encode request and send
	if err = client.cc.Write(&client.header, call.Args); err != nil {
		c := client.removeCall(seq)

		// c is non-nil means call is not handled by server
		if c != nil {
			c.Error = err
			c.done()
		}
	}
}
```

1. 加锁保证报文的完整性
2. 将 Call 添加至待处理映射表
3. 发送请求报文
   - 发送出现错误，则尝试从等待列表中移除 Call
   - 移除成功，则表示 Call 尚未被服务端处理，通知调用方
   - 移除失败，表示 Call 已被处理，无需通知

### 2.5 注册中心

![geerpc registry](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310150950177.jpeg)

注册中心的好处在于，客户端和服务端都只需要感知注册中心的存在，而无需感知对方的存在：

1. 服务端启动后，向注册中心发送注册消息，注册中心得知该服务已经启动，处于可用状态。一般来说，服务端还需要定期向注册中心发送心跳，证明自己还活着。
2. 客户端向注册中心询问，当前哪天服务是可用的，注册中心将可用的服务列表返回客户端。
3. 客户端根据注册中心得到的服务列表，选择其中一个发起调用

```go
// GeeRegistry is a simple register center, provide following functions.
// add a server and receive heartbeat to keep it alive.
// returns all alive servers and delete dead servers sync simultaneously.
type GeeRegistry struct {
	timeout time.Duration
	mu      sync.Mutex
	servers map[string]*ServerItem
}

type ServerItem struct {
	Addr  string
	start time.Time
}
```

- `GeeRegistry`：
  - `timeout`：服务端过期时间
  - `mu`：互斥锁，保证以下字段的并发安全
  - `servers`：服务端列表
- `ServerItem`：表示服务端
  - `Addr`：服务端地址
  - `start`：服务端更新时间，用于计算服务端是否过期

#### 使用 HTTP 协议

```go
func (r *GeeRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    switch req.Method {
    case http.MethodGet:
       w.Header().Set("X-Geerpc-Servers", strings.Join(r.aliveServers(), ","))
    case http.MethodPost:
       addr := req.Header.Get("X-Geerpc-Server")
       if addr == "" {
          w.WriteHeader(http.StatusInternalServerError)
          return
       }
       r.putServer(addr)
    default:
       w.WriteHeader(http.StatusMethodNotAllowed)
    }
}
```

为实现简单通过 HTTP 协议进行服务注册和更新。

#### 心跳

```go
func Heartbeat(registry, addr string, duration time.Duration) {
    if duration == 0 {
       // make sure there is enough time to send heart beat
       // before it's removed from registry
       duration = defaultTimeout - time.Duration(1)*time.Minute
    }
    var err error
    err = sendHeartbeat(registry, addr)
    go func() {
       t := time.Tick(duration)
       for err == nil {
          <-t
          err = sendHeartbeat(registry, addr)
       }
    }()
}
```

1. 对于每个 服务 Server
2. 发送首次心跳，用于注册服务
3. 启用子协程，定时发送心跳，更新服务

### 2.6 服务发现与负载均衡

#### 负载均衡算法

假设有多个服务实例，每个实例提供相同的功能，为了提高整个系统的吞吐量，每个实例部署在不同的机器上。客户端可以选择任意一个实例进行调用，获取想要的结果。那如何选择呢？取决了负载均衡的策略。对于 RPC 框架来说，我们可以很容易地想到这么几种策略：

- 随机选择策略 - 从服务列表中随机选择一个。
- 轮询算法(Round Robin) - 依次调度不同的服务器，每次调度执行 i = (i + 1) mode n。
- 加权轮询(Weight Round Robin) - 在轮询算法的基础上，为每个服务实例设置一个权重，高性能的机器赋予更高的权重，也可以根据服务实例的当前的负载情况做动态的调整，例如考虑最近5分钟部署服务器的 CPU、内存消耗情况。
- 哈希/一致性哈希策略 - 依据请求的某些特征，计算一个 hash 值，根据 hash 值将请求发送到对应的机器。一致性 hash 还可以解决服务实例动态添加情况下，调度抖动的问题。一致性哈希的一个典型应用场景是分布式缓存服务。
- ...

为了简单只实现随机选择和轮询。

#### 服务发现

```go
type SelectMode int

const (
	RandomSelect SelectMode = iota
	RoundRobinSelect
)

type Discovery interface {
	Refresh() error
	Update(servers []string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}
```

`Discovery`接口定义服务发现方法：

- `Refresh`：更新服务列表，与注册中心通信
- `Update`：更新服务列表
- `Get`：根据选择负载均衡策略，获取服务
- `GetAll`：返回服务列表

```go
type MultiServerDiscovery struct {
	r       *rand.Rand   // generate random number
	mu      sync.RWMutex // protect following fields
	servers []string
	index   int // record the selected position for robin algorithm
}

type GeeRegistryDiscovery struct {
	*MultiServerDiscovery
	registry   string
	timeout    time.Duration
	lastUpdate time.Time
}
```

`GeeRegistryDiscovery`实现服务发现：

- `r`：用于随机选择算法
- `servers`：服务地址列表
- `index`：轮询算法索引
- `registry`：注册中心地址
- `timeout`：服务列表过期时间
- `lastUpdate`：服务列表上次更新时间

```go
func (d *GeeRegistryDiscovery) Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServerDiscovery.Get(mode)
}

func (d *MultiServerDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}

	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}
```

`d.index = (d.index + 1) % n`使用轮询算法时，采用模运算保证滚动选择。

### 2.7 XClient

XClient 封装 Client，添加负载均衡和服务发现功能。

```go
type XClient struct {
	d       Discovery
	mode    SelectMode
	opt     *geerpc.Option
	mu      sync.Mutex // protect following
	clients map[string]*geerpc.Client
}
```

- `d`：服务发现模式
- `mode`：负载均衡策略
- `opt`：通信 Option
- `clients`：服务地址对应的 Client，用于 Client 的复用

#### 建立连接

```go
func (xc *XClient) dial(rpcAddr string) (*geerpc.Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	client, ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = client
	}

	return client, nil
}

// XDial calls different functions to connect to a RPC server
// according the first parameter rpcAddr.
// rpcAddr is a general format (protocol@addr) to represent a rpc server
// eg, http@10.0.0.1:7001, tcp@10.0.0.1:9999, unix@/tmp/geerpc.sock
func XDial(rpcAddr string, opts ...*geerpc.Option) (*geerpc.Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return geerpc.DialHTTP("tcp", addr, opts...)
	default:
		// tcp, unix or other transport protocol
		return geerpc.Dial(protocol, addr, opts...)
	}
}
```

- `XDial`：根据不同的协议建立客户端
- `XClient.dial`：若存在已有客户端则复用，否则创建新客户端

#### 发起调用

```go
func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply any) error {
    client, err := xc.dial(rpcAddr)
    if err != nil {
       return err
    }
    return client.Call(ctx, serviceMethod, args, reply)
}

func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply any) error {
    rpcAddr, err := xc.d.Get(xc.mode)
    if err != nil {
       return err
    }
    return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}
```

`Call`：根据负载均衡策略选择服务器

`call`：获取客户端并发起调用

```go
// Broadcast invokes the named function for every server registered in discovery
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply any) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
		e  error
	)
	replyDone := reply == nil
	ctx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var cloneReply any
			if reply != nil {
				cloneReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}

			err := xc.call(rpcAddr, ctx, serviceMethod, args, cloneReply)

			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel()
			}
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(cloneReply).Elem())
				replyDone = true
				cancel()
			}
			mu.Unlock()
		}(rpcAddr)
	}

	wg.Wait()

	cancel()
	return e
}
```

`Broadcast`：向所有服务器发送请求，当一个返回后则终止其余的服务端的处理。

## 3. 流程

### 3.1 服务端注册服务流程

1. 传入结构体实例
2. 通过反射获取符合条件的方法
3. 构建 `Service` 实例，添加至服务端的服务列表

### 3.2 服务端处理流程

1. 无限循环中等待连接
2. 建立连接，启用子协程处理连接
3. 读取 Option 报文，选择编解码方式
4. 无限循环中等待报文流
5. 读取一笔报文，启用子协程处理
6. 获取调用信息，启用子协程开始调用函数
7. 将结果写入响应报文，通知父协程处理结果

![image-20231015091631838](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310150916772.png)

### 3.3 客户端发送和接收流程

1. 通过负载均衡选择服务器
2. 建立连接
3. 发送 Option 报文
4. 启用子协程，等待接收响应
5. 添加 Call 至响应列表
6. 发送请求报文

![image-20231015104307165](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310151043110.png)

## 4. 完整通信流程

![image-20231015105243243](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310151052132.png)

## Reference

1. [七天用Go从零实现系列](https://geektutu.com/post/gee.html)