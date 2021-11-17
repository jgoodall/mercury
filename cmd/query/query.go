package query

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	v1 "code.ornl.gov/situ/mercury/api/v1"
	"code.ornl.gov/situ/mercury/common"
	"github.com/golang/protobuf/ptypes"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type ClientConn struct {
	serverAddr string
	ca         string
	serverName string
	conn       *grpc.ClientConn
	client     v1.PacketServiceClient
}

const (
	ShortQueryTimeFormat = "2006-01-02"
	LongQueryTimeFormat  = time.RFC3339
)

func NewClientConn(serverAddr *net.TCPAddr, ca, name string) *ClientConn {
	return &ClientConn{
		serverAddr: serverAddr.String(),
		ca:         ca,
		serverName: name,
	}
}

func (c *ClientConn) Open(mainCtx context.Context) error {
	creds, err := loadCredentials(c.ca, c.serverAddr, c.serverName)
	if err != nil {
		return err
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock(),
		grpc.FailOnNonTempDialError(true),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(common.GRPCMaxSize), grpc.MaxCallSendMsgSize(common.GRPCMaxSize)),
	}

	timeout := 10 * time.Second

	log.Info().
		Str("server-address", c.serverAddr).
		Str("ca-file", c.ca).
		Str("server-name-override", c.serverName).
		Dur("timeout", timeout).
		Msg("opening client connection")

	ctx, cancelFunc := context.WithTimeout(mainCtx, timeout)
	defer cancelFunc()
	c.conn, err = grpc.DialContext(ctx, c.serverAddr, opts...)
	if err != nil {
		return err
	}

	c.client = v1.NewPacketServiceClient(c.conn)

	return nil
}

func (c *ClientConn) Execute(mainCtx context.Context, label, start string, duration time.Duration, queryType, queryArg string, binOut, showAll bool) error {

	// Try to parse the time in one of the predefined formats.
	var startTime time.Time
	var err error
	if len(start) > 10 {
		startTime, err = time.Parse(LongQueryTimeFormat, start)
		if err != nil {
			return fmt.Errorf("unable to parse start date '%s' using format %s: %s", start, LongQueryTimeFormat, err)
		}
	} else {
		startTime, err = time.Parse(ShortQueryTimeFormat, start)
		if err != nil {
			return fmt.Errorf("unable to parse start date '%s' using format %s: %s", start, ShortQueryTimeFormat, err)
		}
	}

	log.Info().
		Str("component", "query").
		Str("label", label).
		Time("start-time", startTime).
		Dur("duration", duration).
		Str("server-addr", c.serverAddr).
		Str("query-type", queryType).
		Str("query-arg", queryArg).
		Msg("executing index query")

	// Convert golang time.Time to protobyf Timestamp.
	s, err := ptypes.TimestampProto(startTime)
	if err != nil {
		return err
	}

	// Get the QueryType from the string.
	var t v1.QueryType
	switch strings.ToLower(queryType) {
	case "ip":
		t = v1.QueryType_ip
	case "port":
		t = v1.QueryType_port
	case "mac":
		t = v1.QueryType_mac
	case "protocol":
		t = v1.QueryType_protocol
	}

	req := &v1.QueryReq{
		Label:        label,
		ShowAll:      showAll,
		StartTime:    s,
		Duration:     ptypes.DurationProto(duration),
		QueryType:    t,
		Query:        queryArg,
		BinaryOutput: binOut,
	}

	opts := []grpc.CallOption{
		grpc.WaitForReady(true),
		grpc.MaxCallRecvMsgSize(common.GRPCMaxSize),
		grpc.MaxCallSendMsgSize(common.GRPCMaxSize),
	}

	timeout := 90 * time.Second
	ctx, cancelFunc := context.WithTimeout(mainCtx, timeout)
	defer cancelFunc()

	if !binOut {
		stream, err := c.client.QueryStream(ctx, req, opts...)
		if err != nil {
			return err
		}
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("error receiving stream: %s", err)
			}
			outputResponse(resp, showAll)
		}
	} else {
		stream, err := c.client.QueryBinaryStream(ctx, req, opts...)
		if err != nil {
			return err
		}
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("error receiving stream: %s", err)
			}
			r := bytes.NewReader(resp.GetBinary())
			if _, err := io.Copy(os.Stdout, r); err != nil {
				log.Fatal().Err(err).Msg("unable to write binary to stdout")
			}
		}
	}

	return nil
}

func loadCredentials(ca, addr, name string) (credentials.TransportCredentials, error) {
	var creds credentials.TransportCredentials
	if ca == "" {
		creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	} else {
		certPool := x509.NewCertPool()
		ca, err := ioutil.ReadFile(ca)
		if err != nil {
			return nil, fmt.Errorf("error loading TLS CA file %s: %s", ca, err)
		}
		if ok := certPool.AppendCertsFromPEM(ca); !ok {
			return nil, fmt.Errorf("failed to append ca cert")
		}
		serverAddrFields := strings.Split(addr, ":")
		creds = credentials.NewTLS(&tls.Config{
			ServerName: serverAddrFields[0],
			RootCAs:    certPool,
		})
		if name != "" {
			if err := creds.OverrideServerName(name); err != nil {
				return nil, fmt.Errorf("failed to overide server name %s: %s", name, err)
			}
		}
	}
	return creds, nil
}

func (c *ClientConn) Close() error {
	log.Info().
		Str("server-address", c.serverAddr).
		Msg("closing client connection")

	c.conn.Close()

	return nil
}

func outputResponse(resp *v1.QueryResp, showAll bool) {
	if showAll {
		fmt.Printf("%s\n", resp.GetText())
	} else {
		ts, _ := ptypes.Timestamp(resp.GetTimestamp())
		s := fmt.Sprintf("%12s:%-3d", resp.GetSrcIP(), resp.GetSrcPort())
		d := fmt.Sprintf("%12s:%-3d", resp.GetDstIP(), resp.GetDstPort())
		fmt.Printf("%s IP %s > %s %s, len %d\n", ts.Format("2006-01-02 15:04:05.000000"), s, d, resp.Proto, resp.GetLength())
	}
}
