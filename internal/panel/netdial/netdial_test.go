package netdial

import (
	"context"
	"net"
	"testing"
)

// A failed lookup asks the fallback servers, and "off" means none are asked.
func TestFallback(t *testing.T) {
	Use("off")
	if len(fallback) != 0 {
		t.Fatal("off kept fallback servers")
	}
	Use("192.0.2.1, nonsense, 192.0.2.2")
	if len(fallback) != 2 || fallback[0] != "192.0.2.1" {
		t.Fatalf("parsed: %v", fallback)
	}
	// An address is dialled as it is, without a lookup.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	if c, err := DialContext(context.Background(), "tcp", ln.Addr().String()); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	// A name the usual lookup finds isn't sent to the fallback servers (unreachable here).
	if c, err := DialContext(context.Background(), "tcp", "localhost:"+portOf(ln)); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	Use("1.1.1.1,9.9.9.9")
}

func portOf(ln net.Listener) string {
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	return p
}
