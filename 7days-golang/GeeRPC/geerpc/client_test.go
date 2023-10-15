package geerpc

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestClientDialtimeout(t *testing.T) {
	t.Parallel()
	lis, _ := net.Listen("tcp", ":0")

	f := func(conn net.Conn, opt *Option) (*Client, error) {
		_ = conn.Close()
		time.Sleep(2 * time.Second)
		return nil, nil
	}

	t.Run("timeout", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", lis.Addr().String(), &Option{ConnectTimeout: time.Second})
		_assert(err != nil && strings.Contains(err.Error(), "connect timeout"), "expect a timeout error")
	})

	t.Run("0", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", lis.Addr().String(), &Option{ConnectTimeout: 0})
		_assert(err == nil, "0 means no limit")
	})
}

type Bar int

func (b Bar) Timeout(argv int, reply *int) error {
	time.Sleep(2 * time.Second)
	return nil
}

func startServer(addr chan string) {
	var b Bar
	_ = Register(&b)
	lis, _ := net.Listen("tcp", ":0")
	addr <- lis.Addr().String()
	Accept(lis)
}

func TestClient_Call(t *testing.T) {
	t.Parallel()
	addrCh := make(chan string)
	go startServer(addrCh)
	addr := <-addrCh

	t.Run("client timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.Call(ctx, "Bar.Timeout", 1, reply)
		_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expect a timeout error")
	})

	t.Run("server handle timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr, &Option{
			HandleTimeout: time.Second,
		})
		var reply int
		err := client.Call(context.Background(), "Bar.Timeout", 1, &reply)
		_assert(err != nil && strings.Contains(err.Error(), "handle timeout"), "expect a timeout error")
	})
}
