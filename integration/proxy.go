package integration

import (
	"io"
	"net"
	"sync"
	"testing"
)

// Forwarder is a loopback TCP relay between the service and one of its
// backing services, the seam a test injects an outage through: the service
// is configured against the forwarder's address instead of the backing
// service's, Sever refuses new connections and drops the open ones, and
// Restore listens again on the same address so the service reconnects. It
// relays bytes and knows nothing of the protocol, so the database is only
// its first tenant; any TCP-backed capability the service adds takes the
// same seam. The service cannot tell it from the network.
type Forwarder struct {
	target string

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

// Forward starts relaying to target, host:port, on an ephemeral loopback
// port; the relay is closed at test cleanup.
func Forward(t testing.TB, target string) *Forwarder {
	t.Helper()
	f := &Forwarder{target: target, conns: map[net.Conn]struct{}{}}
	if err := f.listen(""); err != nil {
		t.Fatalf("forwarder: %v", err)
	}
	t.Cleanup(f.Close)
	return f
}

// Addr is the address the service connects to, host:port. It is stable
// across Sever and Restore.
func (f *Forwarder) Addr() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listener.Addr().String()
}

func (f *Forwarder) listen(addr string) error {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.listener = l
	f.mu.Unlock()
	f.wg.Add(1)
	go f.accept(l)
	return nil
}

func (f *Forwarder) accept(l net.Listener) {
	defer f.wg.Done()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", f.target)
		if err != nil {
			_ = c.Close()
			continue
		}
		f.track(c, up)
		go f.relay(c, up)
	}
}

func (f *Forwarder) track(conns ...net.Conn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range conns {
		f.conns[c] = struct{}{}
	}
}

func (f *Forwarder) untrack(conns ...net.Conn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range conns {
		delete(f.conns, c)
		_ = c.Close()
	}
}

// relay copies both directions until either side closes.
func (f *Forwarder) relay(down, up net.Conn) {
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go pipe(up, down)
	go pipe(down, up)
	<-done
	f.untrack(down, up)
	<-done
}

// Sever is the outage: the listener closes, so new connections are refused,
// and every relayed connection is dropped, so in-flight and pooled
// connections fail on their next use. The accept loop is waited for before
// the drop, so a connection accepted as the listener closed is tracked and
// dropped with the rest. The address stays reserved for Restore.
func (f *Forwarder) Sever() {
	f.mu.Lock()
	l := f.listener
	f.mu.Unlock()
	_ = l.Close()
	f.wg.Wait()

	f.mu.Lock()
	conns := make([]net.Conn, 0, len(f.conns))
	for c := range f.conns {
		conns = append(conns, c)
	}
	f.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Restore ends the outage: the forwarder listens again on the address it
// had, and the service's next connection attempt reaches the database.
func (f *Forwarder) Restore(t testing.TB) {
	t.Helper()
	addr := f.Addr()
	if err := f.listen(addr); err != nil {
		t.Fatalf("forwarder restore on %s: %v", addr, err)
	}
}

// Close severs and releases the forwarder.
func (f *Forwarder) Close() { f.Sever() }
