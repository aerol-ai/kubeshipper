package kube

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// ServiceSpec mirrors the JSON contract previously defined in Zod.
// Validation lives here (one source of truth instead of zod + ts types).
type ServiceSpec struct {
	Name            string             `json:"name"`
	Namespace       string             `json:"namespace,omitempty"`
	Image           string             `json:"image"`
	Port            *int               `json:"port,omitempty"`
	Env             map[string]string  `json:"env,omitempty"`
	Replicas        *int               `json:"replicas,omitempty"`
	Public          bool               `json:"public,omitempty"`
	Hostname        string             `json:"hostname,omitempty"`
	ImagePullSecret string             `json:"imagePullSecret,omitempty"`
	Resources       *ResourceRequests  `json:"resources,omitempty"`
	Type            string             `json:"type,omitempty"` // service|job|cronjob (only "service" used today)
	Schedule        string             `json:"schedule,omitempty"`
}

type ResourceRequests struct {
	Requests map[string]string `json:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"`
}

var dns1035 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func ParseServiceSpec(body []byte) (*ServiceSpec, error) {
	var s ServiceSpec
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *ServiceSpec) Validate() error {
	if len(s.Name) == 0 || len(s.Name) > 63 || !dns1035.MatchString(s.Name) {
		return fmt.Errorf("name must be a valid DNS-1035 label (1-63 chars, lowercase alnum + dashes)")
	}
	if s.Image == "" {
		return fmt.Errorf("image is required")
	}
	if s.Port != nil && (*s.Port < 1 || *s.Port > 65535) {
		return fmt.Errorf("port must be 1-65535")
	}
	if s.Replicas != nil && *s.Replicas < 0 {
		return fmt.Errorf("replicas must be >= 0")
	}
	if s.Type == "" {
		s.Type = "service"
	}
	if s.Type != "service" && s.Type != "job" && s.Type != "cronjob" {
		return fmt.Errorf("type must be service|job|cronjob")
	}
	if s.Replicas == nil {
		one := 1
		s.Replicas = &one
	}
	return nil
}

func (s *ServiceSpec) Merge(patch *ServiceSpec) *ServiceSpec {
	merged := *s
	if patch.Image != "" {
		merged.Image = patch.Image
	}
	if patch.Port != nil {
		merged.Port = patch.Port
	}
	if patch.Env != nil {
		merged.Env = patch.Env
	}
	if patch.Replicas != nil {
		merged.Replicas = patch.Replicas
	}
	if patch.Hostname != "" {
		merged.Hostname = patch.Hostname
	}
	if patch.Namespace != "" {
		merged.Namespace = patch.Namespace
	}
	if patch.ImagePullSecret != "" {
		merged.ImagePullSecret = patch.ImagePullSecret
	}
	if patch.Resources != nil {
		merged.Resources = patch.Resources
	}
	if patch.Type != "" {
		merged.Type = patch.Type
	}
	if patch.Schedule != "" {
		merged.Schedule = patch.Schedule
	}
	merged.Public = patch.Public || s.Public
	return &merged
}
