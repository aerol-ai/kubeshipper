package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	pb "kubeshipper/helmd/gen"
	"kubeshipper/helmd/internal/server"
)

func main() {
	socketPath := flag.String("socket", "/tmp/helmd.sock", "unix domain socket path")
	flag.Parse()

	_ = os.Remove(*socketPath)

	lis, err := net.Listen("unix", *socketPath)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	if err := os.Chmod(*socketPath, 0600); err != nil {
		log.Fatalf("chmod socket: %v", err)
	}

	srv, err := server.New()
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	g := grpc.NewServer(grpc.MaxRecvMsgSize(64 * 1024 * 1024))
	pb.RegisterHelmdServer(g, srv)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("helmd: shutting down")
		g.GracefulStop()
	}()

	log.Printf("helmd: listening on %s", *socketPath)
	if err := g.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
