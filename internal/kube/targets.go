package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentTargetContainer struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	TrackedImage string `json:"tracked_image"`
}

type DeploymentTarget struct {
	Namespace  string                      `json:"namespace"`
	Deployment string                      `json:"deployment"`
	Service    string                      `json:"service,omitempty"`
	Containers []DeploymentTargetContainer `json:"containers"`
}

func (c *Client) ListManagedDeploymentTargets(ctx context.Context, namespace string) ([]DeploymentTarget, error) {
	var namespaces []string
	if strings.TrimSpace(namespace) != "" {
		ns, err := c.ResolveNamespace(namespace)
		if err != nil {
			return nil, err
		}
		namespaces = []string{ns}
	} else {
		for ns := range c.Managed {
			namespaces = append(namespaces, ns)
		}
		sort.Strings(namespaces)
	}

	out := make([]DeploymentTarget, 0)
	for _, ns := range namespaces {
		list, err := c.KC.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list deployments in %s: %w", ns, err)
		}
		sort.Slice(list.Items, func(i, j int) bool {
			return list.Items[i].Name < list.Items[j].Name
		})
		for _, deployment := range list.Items {
			target := DeploymentTarget{
				Namespace:  ns,
				Deployment: deployment.Name,
				Service:    deployment.Name,
				Containers: make([]DeploymentTargetContainer, 0, len(deployment.Spec.Template.Spec.Containers)),
			}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				target.Containers = append(target.Containers, DeploymentTargetContainer{
					Name:         container.Name,
					Image:        container.Image,
					TrackedImage: trackedImageForWatch(container.Image),
				})
			}
			out = append(out, target)
		}
	}

	return out, nil
}
