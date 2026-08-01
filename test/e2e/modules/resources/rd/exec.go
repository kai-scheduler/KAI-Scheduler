/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"bytes"
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const execRetries = 10

// ExecInPod runs a command inside the first container of a pod and returns stdout.
func ExecInPod(
	ctx context.Context,
	client *kubernetes.Clientset,
	config *rest.Config,
	pod *v1.Pod,
	command []string,
) string {
	var lastErr error
	for i := 0; i < execRetries; i++ {
		out, err := doExecInPod(ctx, client, config, pod, command)
		if err == nil {
			return out
		}
		lastErr = err
		time.Sleep(time.Second)
	}
	panic(fmt.Sprintf("ExecInPod failed after %d retries for pod %s/%s: %v",
		execRetries, pod.Namespace, pod.Name, lastErr))
}

func doExecInPod(
	ctx context.Context,
	client *kubernetes.Clientset,
	config *rest.Config,
	pod *v1.Pod,
	command []string,
) (string, error) {
	if len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("pod %s/%s has no containers", pod.Namespace, pod.Name)
	}

	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: pod.Spec.Containers[0].Name,
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    false,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %w", err)
	}

	var outBuf bytes.Buffer
	if err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &outBuf,
	}); err != nil {
		return "", err
	}
	return outBuf.String(), nil
}
