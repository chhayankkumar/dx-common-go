package client

import (
	"testing"

	"google.golang.org/grpc/credentials/insecure"
)

func TestDial(t *testing.T) {
	t.Run("empty target errors", func(t *testing.T) {
		if _, err := Dial(Config{}); err == nil {
			t.Fatal("expected error for empty target")
		}
	})

	t.Run("lazy dial returns a conn without a live server", func(t *testing.T) {
		// grpc.NewClient is lazy — it does not block on connectivity.
		conn, err := Dial(Config{Target: "127.0.0.1:0"})
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		defer conn.Close()
		if conn == nil {
			t.Fatal("expected a non-nil conn")
		}
	})

	t.Run("options apply without panicking", func(t *testing.T) {
		conn, err := Dial(Config{Target: "127.0.0.1:0"},
			WithoutTracing(), WithoutResilience())
		if err != nil {
			t.Fatalf("Dial with options: %v", err)
		}
		conn.Close()
	})
}

func TestTransportCredentials(t *testing.T) {
	t.Run("insecure by default", func(t *testing.T) {
		c, err := transportCredentials(Config{})
		if err != nil {
			t.Fatalf("transportCredentials: %v", err)
		}
		if c.Info().SecurityProtocol != insecure.NewCredentials().Info().SecurityProtocol {
			t.Fatalf("expected insecure credentials, got %q", c.Info().SecurityProtocol)
		}
	})

	t.Run("TLS with a bad CA path errors", func(t *testing.T) {
		if _, err := transportCredentials(Config{TLS: true, CACertPath: "/no/such/ca.pem"}); err == nil {
			t.Fatal("expected error for missing CA file")
		}
	})

	t.Run("TLS without a CA uses system roots", func(t *testing.T) {
		if _, err := transportCredentials(Config{TLS: true}); err != nil {
			t.Fatalf("TLS with system roots: %v", err)
		}
	})
}
