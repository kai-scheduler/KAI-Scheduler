// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Read the real kubelet API from inside a disposable Kind node.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	podresources "k8s.io/kubelet/pkg/apis/podresources/v1"
)

func main() {
	if err := dump(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dump() (result error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := grpc.NewClient("unix:///var/lib/kubelet/pod-resources/kubelet.sock", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, connection.Close()) }()
	client := podresources.NewPodResourcesListerClient(connection)
	pods, err := client.List(ctx, &podresources.ListPodResourcesRequest{})
	if err != nil {
		return fmt.Errorf("list pod resources: %w", err)
	}
	allocatable, err := client.GetAllocatableResources(ctx, &podresources.AllocatableResourcesRequest{})
	if err != nil {
		return fmt.Errorf("get allocatable resources: %w", err)
	}
	options := protojson.MarshalOptions{EmitDefaultValues: true}
	podJSON, err := options.Marshal(pods)
	if err != nil {
		return err
	}
	allocatableJSON, err := options.Marshal(allocatable)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]json.RawMessage{"pods": podJSON, "allocatable": allocatableJSON})
}
