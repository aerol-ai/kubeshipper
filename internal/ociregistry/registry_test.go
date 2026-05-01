package ociregistry

import "testing"

func TestBasicAuthUsesPasswordOrToken(t *testing.T) {
	t.Run("password wins when present", func(t *testing.T) {
		username, secret, ok, err := BasicAuth(&Auth{
			Username: "octocat",
			Password: "secret-password",
			Token:    "ghp_token",
		})
		if err != nil {
			t.Fatalf("basic auth: %v", err)
		}
		if !ok {
			t.Fatal("expected credentials to be accepted")
		}
		if username != "octocat" {
			t.Fatalf("username: got %q", username)
		}
		if secret != "secret-password" {
			t.Fatalf("secret: got %q", secret)
		}
	})

	t.Run("token works as password fallback", func(t *testing.T) {
		username, secret, ok, err := BasicAuth(&Auth{
			Username: "octocat",
			Token:    "ghp_token",
		})
		if err != nil {
			t.Fatalf("basic auth: %v", err)
		}
		if !ok {
			t.Fatal("expected credentials to be accepted")
		}
		if username != "octocat" {
			t.Fatalf("username: got %q", username)
		}
		if secret != "ghp_token" {
			t.Fatalf("secret: got %q", secret)
		}
	})
}

func TestBasicAuthValidation(t *testing.T) {
	if _, _, ok, err := BasicAuth(nil); err != nil || ok {
		t.Fatalf("nil auth: ok=%v err=%v", ok, err)
	}

	if _, _, _, err := BasicAuth(&Auth{Token: "ghp_token"}); err == nil {
		t.Fatal("expected username validation error")
	}

	if _, _, _, err := BasicAuth(&Auth{Username: "octocat"}); err == nil {
		t.Fatal("expected secret validation error")
	}
}
