package main

import (
	"context"
	"geerpc"
	"geerpc/registry"
	"geerpc/xclient"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Duration(args.Num1) * time.Second)
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(regAddr string, wg *sync.WaitGroup) {
	var f Foo
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("server listen tcp failed:", err)
	}

	server := geerpc.NewServer()
	if err = server.Register(&f); err != nil {
		log.Fatal("server listen tcp failed:", err)
	}

	registry.Heartbeat(regAddr, "tcp@"+lis.Addr().String(), 0)

	log.Println("server runs at:", lis.Addr().String())
	wg.Done()
	server.Accept(lis)
}

func startRegistry(wg *sync.WaitGroup) {
	lis, _ := net.Listen("tcp", ":9999")
	registry.HandleHTTP()
	wg.Done()
	_ = http.Serve(lis, nil)
}

func foo(xc *xclient.XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
	var (
		reply int
		err   error
	)

	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	case "broadcast":
		err = xc.Broadcast(ctx, serviceMethod, args, &reply)
	}

	if err != nil {
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}

func call(regAddr string) {
	d := xclient.NewGeeRegistryDiscovery(regAddr, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "call", "Foo.Sum", &Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}

func broadcast(regAddr string) {
	d := xclient.NewGeeRegistryDiscovery(regAddr, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "broadcast", "Foo.Sum", &Args{Num1: i, Num2: i * i})
			// expect 2 - 5 timeout
			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
			foo(xc, ctx, "broadcast", "Foo.Sleep", &Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}

func main() {
	log.SetFlags(0)
	regAddr := "http://localhost:9999/geerpc/registry"
	var wg sync.WaitGroup

	wg.Add(1)
	go startRegistry(&wg)
	wg.Wait()

	wg.Add(2)
	go startServer(regAddr, &wg)
	go startServer(regAddr, &wg)
	wg.Wait()

	call(regAddr)
	broadcast(regAddr)
}
