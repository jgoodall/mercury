package serve

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/rs/cors"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	v1 "code.ornl.gov/situ/mercury/api/v1"
	"code.ornl.gov/situ/mercury/common"
)

type QueryServer struct {
	ctx        context.Context
	done       chan<- struct{}
	grpcServer *grpc.Server
	httpServer *http.Server
	grpcPort   uint16
	httpPort   uint16
	cert       string
	key        string
	serverName string
	indexPath  string
	pcapPaths  []string
}

func NewQueryServer(grpcPort, httpPort uint16, cert, key, serverName, indexPath string, pcapPaths []string) *QueryServer {
	return &QueryServer{
		grpcPort:   grpcPort,
		cert:       cert,
		key:        key,
		serverName: serverName,
		httpPort:   httpPort,
		indexPath:  indexPath,
		pcapPaths:  pcapPaths,
	}
}

// TODO - return grpc status object for errors
func (s *QueryServer) Run(ctx context.Context, done chan<- struct{}) (err error) {

	s.ctx = ctx
	s.done = done

	// Validate path to certificates and key files.
	if _, err = os.Stat(s.cert); os.IsNotExist(err) {
		return fmt.Errorf("tls cert file '%s' does not exist", s.cert)
	}
	if _, err = os.Stat(s.key); os.IsNotExist(err) {
		return fmt.Errorf("tls key file '%s' does not exist", s.key)
	}

	go func() {
		addr := fmt.Sprintf(":%d", s.grpcPort)
		listen, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal().Err(err).Str("grpc-address", addr).Msg("unable to start grpc listen")
		}
		creds, err := credentials.NewServerTLSFromFile(s.cert, s.key)
		if err != nil {
			log.Fatal().Err(err).Str("cert", s.cert).Str("key", s.key).Msg("unable to load certificate")
		}
		opts := []grpc.ServerOption{
			grpc.Creds(creds),
			grpc.MaxSendMsgSize(common.GRPCMaxSize),
			grpc.MaxRecvMsgSize(common.GRPCMaxSize),
		}
		s.grpcServer = grpc.NewServer(opts...)
		packetQueryService := NewPacketQueryService(s.indexPath, s.pcapPaths)
		v1.RegisterPacketServiceServer(s.grpcServer, packetQueryService)
		log.Info().
			Str("grpc-addr", addr).
			Str("cert-file", s.cert).
			Str("key-file", s.key).
			Msg("starting grpc query server")
		err = s.grpcServer.Serve(listen)
		if err != nil {
			log.Fatal().Err(err).Str("grpc-addr", addr).Msg("unable to start grpc server")
		}
	}()

	grpcServerAddr := fmt.Sprintf("localhost:%d", s.grpcPort)
	mux := runtime.NewServeMux()
	creds, err := credentials.NewClientTLSFromFile(s.cert, s.serverName)
	if err != nil {
		log.Fatal().Err(err).Str("cert-file", s.cert).Str("server-name", s.serverName).Msg("unable to create TLS client from cert")
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(common.GRPCMaxSize), grpc.MaxCallSendMsgSize(common.GRPCMaxSize),
		),
	}
	err = v1.RegisterPacketServiceHandlerFromEndpoint(context.Background(), mux, grpcServerAddr, opts)
	if err != nil {
		log.Fatal().Err(err).Str("grpc-address", grpcServerAddr).Msg("register handler from grpc endpoint")
	}
	addr := fmt.Sprintf(":%d", s.httpPort)
	handler := cors.Default().Handler(mux) // Handle CORS requests
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	log.Info().
		Uint16("http-port", s.httpPort).
		Str("cert-file", s.cert).
		Msg("starting http gateway")

	go s.httpServer.ListenAndServe()

	<-s.ctx.Done()
	s.Stop()

	return nil
}

func (s *QueryServer) Stop() {
	log.Info().
		Str("grpc-address", fmt.Sprintf(":%d", s.grpcPort)).
		Str("http-address", fmt.Sprintf(":%d", s.httpPort)).
		Msg("stopping query server")

	err := s.httpServer.Shutdown(context.Background())
	if err != nil {
		log.Warn().
			Str("http-address", fmt.Sprintf(":%d", s.httpPort)).
			Err(err).
			Msg("error stopping http query server")
	}

	s.grpcServer.GracefulStop()
	s.done <- struct{}{}
}
