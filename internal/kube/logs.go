package kube

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StreamPodLogs streams logs from the first pod matching app=name into w.
// Caller is responsible for keeping the request alive (HTTP handler holds the goroutine).
func (c *Client) StreamPodLogs(ctx context.Context, namespace, name string, w io.Writer) error {
	ns, err := c.ResolveNamespace(namespace)
	if err != nil {
		return err
	}
	pods, err := c.KC.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + name,
	})
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		_, _ = fmt.Fprintf(w, "No pods found for %s\n", name)
		return nil
	}

	pod := pods.Items[0].Name
	tail := int64(50)
	req := c.KC.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
		Container: "app",
		Follow:    true,
		TailLines: &tail,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("open log stream: %w", err)
	}
	defer stream.Close()

	_, err = io.Copy(w, stream)
	return err
}
