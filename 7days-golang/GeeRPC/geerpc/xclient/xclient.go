package xclient

import (
	"context"
	"fmt"
	"geerpc"
	"io"
	"reflect"
	"strings"
	"sync"
)

type XClient struct {
	d       Discovery
	mode    SelectMode
	opt     *geerpc.Option
	mu      sync.Mutex // protect following
	clients map[string]*geerpc.Client
}

var _ io.Closer = (*XClient)(nil)

func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key)
	}

	return nil
}

func NewXClient(d Discovery, mode SelectMode, opt *geerpc.Option) *XClient {
	return &XClient{
		d:       d,
		mode:    mode,
		opt:     opt,
		clients: make(map[string]*geerpc.Client),
	}
}

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
