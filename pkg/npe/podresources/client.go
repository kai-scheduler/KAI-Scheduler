// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package podresources is a thin client over the kubelet podresources gRPC API, exposing the
// per-pod, per-container resource allocations (devices with NUMA topology, exclusive CPU IDs,
// and pinned memory blocks) the exporter needs to attribute placement.
package podresources

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
)

// maxMsgSize bounds the List response. On large nodes the podresources payload can be sizable;
// this matches the kubelet's own default ceiling.
const maxMsgSize = 1024 * 1024 * 16

// Client lists pod resource allocations from the local kubelet.
type Client struct {
	conn   *grpc.ClientConn
	client podresourcesv1.PodResourcesListerClient
}

// New dials the kubelet podresources unix socket and returns a ready Client.
func New(socketPath string) (*Client, error) {
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMsgSize)),
	)
	if err != nil {
		return nil, fmt.Errorf("dialing podresources socket %q: %w", socketPath, err)
	}

	return &Client{
		conn:   conn,
		client: podresourcesv1.NewPodResourcesListerClient(conn),
	}, nil
}

// List returns the resource allocations of every pod the kubelet currently admits on this node.
func (c *Client) List(ctx context.Context) ([]*podresourcesv1.PodResources, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.client.List(ctx, &podresourcesv1.ListPodResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("listing pod resources: %w", err)
	}
	return resp.GetPodResources(), nil
}

// Close releases the gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
