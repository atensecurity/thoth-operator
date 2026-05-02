package thoth

import "testing"

func TestNewClientDerivesAPIBaseURLFromTenantAndApex(t *testing.T) {
	client, err := NewClient(ClientOptions{
		TenantID:   "acme",
		ApexDomain: "atensecurity.com",
		AuthToken:  "token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if got, want := client.EndpointURL(), "https://grid.acme.atensecurity.com"; got != want {
		t.Fatalf("EndpointURL() = %q, want %q", got, want)
	}
}

func TestNewClientUsesExplicitAPIBaseURL(t *testing.T) {
	client, err := NewClient(ClientOptions{
		TenantID:   "acme",
		APIBaseURL: "https://custom.example.com",
		AuthToken:  "token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if got, want := client.EndpointURL(), "https://custom.example.com"; got != want {
		t.Fatalf("EndpointURL() = %q, want %q", got, want)
	}
}

func TestNewClientRequiresTenantAndToken(t *testing.T) {
	if _, err := NewClient(ClientOptions{AuthToken: "token"}); err == nil {
		t.Fatalf("expected error when tenant is missing")
	}
	if _, err := NewClient(ClientOptions{TenantID: "acme"}); err == nil {
		t.Fatalf("expected error when token is missing")
	}
}
