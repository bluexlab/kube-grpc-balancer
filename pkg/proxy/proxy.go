package proxy

import (
	"context"
	"io"
	"net"

	"github.com/sercand/kuberesolver/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	clientStreamDescForProxying = &grpc.StreamDesc{
		ServerStreams: true,
		ClientStreams: true,
	}
)

type Proxy struct {
	listener   net.Listener
	upstream   *grpc.Server
	downstream *grpc.ClientConn
}

func init() {
	kuberesolver.RegisterInCluster()
}

func NewProxy(serviceAddress, downStreamAddress string) (*Proxy, error) {
	p := &Proxy{}

	listener, err := net.Listen("tcp", serviceAddress)
	if err != nil {
		return nil, err
	}
	grpcServer := grpc.NewServer(
		grpc.UnknownServiceHandler(p.serverHandler),
	)

	serviceConfig := `{
		"methodConfig": [
			{
				"name": [],
				"retryPolicy": {
					"maxAttempts": 5,
					"initialBackoff": "0.1s",
					"maxBackoff": "1s",
					"backoffMultiplier": 2,
					"retryableStatusCodes": ["RESOURCE_EXHAUSTED", "UNAVAILABLE"]
				}
			}
		],
		"loadBalancingConfig":[{ "round_robin": {} }]
	}`
	clientConn, err := grpc.Dial(
		downStreamAddress,
		grpc.WithDefaultServiceConfig(serviceConfig),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	p.listener = listener
	p.upstream = grpcServer
	p.downstream = clientConn
	return p, nil
}

func (p *Proxy) Serve() error {
	return p.upstream.Serve(p.listener)
}

func (p *Proxy) Stop() {
	p.upstream.GracefulStop()
}

// The whole function is highly based on https://github.com/mwitkow/grpc-proxy.
func (p *Proxy) serverHandler(srv interface{}, serverStream grpc.ServerStream) error {
	// md, _ := metadata.FromIncomingContext(stream.Context())
	fullMethodName, ok := grpc.MethodFromServerStream(serverStream)
	if !ok {
		return status.Errorf(codes.Internal, "lowLevelServerStream not exists in context")
	}

	// Copy metadata from server stream to client stream.
	md, _ := metadata.FromIncomingContext(serverStream.Context())
	clientCtx := metadata.NewOutgoingContext(serverStream.Context(), md.Copy())
	clientCtx, clientCancel := context.WithCancel(clientCtx)
	defer clientCancel()

	clientStream, err := grpc.NewClientStream(clientCtx, clientStreamDescForProxying, p.downstream, fullMethodName)
	if err != nil {
		return err
	}

	// Explicitly *do not close* s2cErrChan and c2sErrChan, otherwise the select below will not terminate.
	// Channels do not have to be closed, it is just a control flow mechanism, see
	// https://groups.google.com/forum/#!msg/golang-nuts/pZwdYRGxCIk/qpbHxRRPJdUJ
	s2cErrChan := p.forwardServerToClient(serverStream, clientStream)
	c2sErrChan := p.forwardClientToServer(clientStream, serverStream)
	// We don't know which side is going to stop sending first, so we need a select between the two.
	for i := 0; i < 2; i++ {
		select {
		case s2cErr := <-s2cErrChan:
			if s2cErr == io.EOF {
				// this is the happy case where the sender has encountered io.EOF, and won't be sending anymore./
				// the clientStream>serverStream may continue pumping though.
				clientStream.CloseSend()
			} else {
				// however, we may have gotten a receive error (stream disconnected, a read error etc) in which case we need
				// to cancel the clientStream to the backend, let all of its goroutines be freed up by the CancelFunc and
				// exit with an error to the stack
				clientCancel()
				return status.Errorf(codes.Internal, "failed proxying s2c: %v", s2cErr)
			}
		case c2sErr := <-c2sErrChan:
			// This happens when the clientStream has nothing else to offer (io.EOF), returned a gRPC error. In those two
			// cases we may have received Trailers as part of the call. In case of other errors (stream closed) the trailers
			// will be nil.
			serverStream.SetTrailer(clientStream.Trailer())
			// c2sErr will contain RPC error from client code. If not io.EOF return the RPC error as server stream error.
			if c2sErr != io.EOF {
				return c2sErr
			}
			return nil
		}
	}

	return nil
}

func (p *Proxy) forwardClientToServer(src grpc.ClientStream, dst grpc.ServerStream) chan error {
	ret := make(chan error, 1)
	go func() {
		f := &emptypb.Empty{}
		for i := 0; ; i++ {
			if err := src.RecvMsg(f); err != nil {
				ret <- err // this can be io.EOF which is happy case
				break
			}
			if i == 0 {
				// This is a bit of a hack, but client to server headers are only readable after first client msg is
				// received but must be written to server stream before the first msg is flushed.
				// This is the only place to do it nicely.
				md, err := src.Header()
				if err != nil {
					ret <- err
					break
				}
				if err := dst.SendHeader(md); err != nil {
					ret <- err
					break
				}
			}
			if err := dst.SendMsg(f); err != nil {
				ret <- err
				break
			}
		}
	}()
	return ret
}

func (p *Proxy) forwardServerToClient(src grpc.ServerStream, dst grpc.ClientStream) chan error {
	ret := make(chan error, 1)
	go func() {
		f := &emptypb.Empty{}
		for i := 0; ; i++ {
			if err := src.RecvMsg(f); err != nil {
				ret <- err // this can be io.EOF which is happy case
				break
			}
			if err := dst.SendMsg(f); err != nil {
				ret <- err
				break
			}
		}
	}()
	return ret
}
