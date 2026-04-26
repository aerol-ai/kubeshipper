package server

import (
	"context"
	"fmt"

	pb "kubeshipper/helmd/gen"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// applyPrereqSecrets creates or updates the secrets the chart depends on (Cloudflare API token,
// custom registry pull secrets, etc.) before install. Idempotent.
func (s *Server) applyPrereqSecrets(ctx context.Context, secrets []*pb.PrereqSecret) error {
	for _, sec := range secrets {
		if sec.Namespace == "" || sec.Name == "" {
			return fmt.Errorf("prereq secret missing namespace or name")
		}

		// Ensure namespace exists.
		if _, err := s.kc.CoreV1().Namespaces().Get(ctx, sec.Namespace, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				_, err := s.kc.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: sec.Namespace},
				}, metav1.CreateOptions{})
				if err != nil && !apierrors.IsAlreadyExists(err) {
					return fmt.Errorf("create ns %s: %w", sec.Namespace, err)
				}
			} else {
				return fmt.Errorf("get ns %s: %w", sec.Namespace, err)
			}
		}

		secType := corev1.SecretTypeOpaque
		if sec.Type != "" {
			secType = corev1.SecretType(sec.Type)
		}
		obj := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sec.Name,
				Namespace: sec.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kubeshipper",
				},
			},
			StringData: sec.StringData,
			Type:       secType,
		}

		existing, err := s.kc.CoreV1().Secrets(sec.Namespace).Get(ctx, sec.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = s.kc.CoreV1().Secrets(sec.Namespace).Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("create secret %s/%s: %w", sec.Namespace, sec.Name, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("get secret %s/%s: %w", sec.Namespace, sec.Name, err)
		}
		obj.ResourceVersion = existing.ResourceVersion
		_, err = s.kc.CoreV1().Secrets(sec.Namespace).Update(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update secret %s/%s: %w", sec.Namespace, sec.Name, err)
		}
	}
	return nil
}
