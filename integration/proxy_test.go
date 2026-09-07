package integration_test

import (
	"bufio"
	"errors"
	"net"
	"testing"

	"github.com/standards-lab/go-web-service/integration"
)

// echo serves one line back per connection for the forwarder to relay.
func echo(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err == nil {
					_, _ = c.Write([]byte(line))
				}
			}()
		}
	}()
	return l.Addr().String()
}

func roundTrip(t *testing.T, addr, line string) (string, error) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write([]byte(line + "\n")); err != nil {
		return "", err
	}
	got, err := bufio.NewReader(c).ReadString('\n')
	return got, err
}

// The forwarder relays while up, refuses while severed, and relays again on
// the same address after Restore.
func TestForwarder_SeverAndRestore(t *testing.T) {
	f := integration.Forward(t, echo(t))
	addr := f.Addr()

	if got, err := roundTrip(t, addr, "hello"); err != nil || got != "hello\n" {
		t.Fatalf("relay = %q, %v", got, err)
	}

	f.Sever()
	var opErr *net.OpError
	if _, err := roundTrip(t, addr, "down"); !errors.As(err, &opErr) {
		t.Fatalf("severed forwarder answered: %v", err)
	}

	f.Restore(t)
	if f.Addr() != addr {
		t.Fatalf("address changed on restore: %s -> %s", addr, f.Addr())
	}
	if got, err := roundTrip(t, addr, "back"); err != nil || got != "back\n" {
		t.Fatalf("relay after restore = %q, %v", got, err)
	}
}

// Sever drops an open relayed connection, not only new ones: a pooled
// database connection fails on its next use.
func TestForwarder_SeverDropsOpenConnections(t *testing.T) {
	f := integration.Forward(t, echo(t))
	c, err := net.Dial("tcp", f.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	f.Sever()
	if _, err := bufio.NewReader(c).ReadString('\n'); err == nil {
		t.Fatal("open connection survived the outage")
	}
}
