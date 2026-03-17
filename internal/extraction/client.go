package extraction

import (
	"context"
	"fmt"
	"time"

	pb "github.com/gauravfs-14/webhookmind/internal/extraction/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ExtractionClient struct {
	conn    *grpc.ClientConn
	client  pb.ExtractionServiceClient
	timeout time.Duration
}

func NewExtractionClient(addr string, timeoutSeconds int) (*ExtractionClient, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", addr, err)
	}

	return &ExtractionClient{
		conn:    conn,
		client:  pb.NewExtractionServiceClient(conn),
		timeout: time.Duration(timeoutSeconds) * time.Second,
	}, nil
}

func (c *ExtractionClient) Extract(ctx context.Context, req *pb.ExtractionRequest) (*pb.ExtractionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.client.Extract(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("grpc extract: %w", err)
	}
	return resp, nil
}

func (c *ExtractionClient) Close() error {
	return c.conn.Close()
}
