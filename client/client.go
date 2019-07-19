package client

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/walkert/ceedee/ceedeeproto"
	"google.golang.org/grpc"
)

type Client struct {
	c pb.CeeDeeClient
}

func (c *Client) Get(dir string) ([]string, error) {
	dlist, err := c.c.Get(context.Background(), &pb.Directory{Name: dir})
	if err != nil {
		if strings.Contains(err.Error(), "No entry for") {
			return []string{}, nil
		}
		return []string{}, err
	}
	return strings.Split(dlist.Dirs, ":"), nil
}

func New(port int) (*Client, error) {
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", port), grpc.WithInsecure())
	if err != nil {
		return &Client{}, fmt.Errorf("coult not connect to server: %v\n", err)
	}
	return &Client{c: pb.NewCeeDeeClient(conn)}, nil
}
