package agent

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// A stand-in Minecraft server: reads the handshake and status request, answers with a status.
func TestQueryMinecraft(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		size, _ := readVarint(r) // handshake
		handshake := make([]byte, size)
		_, _ = io.ReadFull(r, handshake)
		if next := handshake[len(handshake)-1]; handshake[0] != 0 || next != 1 {
			return
		}
		size, _ = readVarint(r) // status request
		_, _ = io.ReadFull(r, make([]byte, size))
		status := []byte(`{"version":{"name":"Paper 1.21.4","protocol":769},"players":{"max":20,"online":2,"sample":[{"name":"Alex","id":"a"},{"name":"Robin\n","id":"b"}]},"description":"A server"}`)
		var body bytes.Buffer
		body.WriteByte(0)
		body.Write(varint(int32(len(status))))
		body.Write(status)
		_, _ = conn.Write(append(varint(int32(body.Len())), body.Bytes()...))
	}()
	res, err := queryMinecraft(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if res.Online != 2 || res.Max != 20 || len(res.Names) != 2 || res.Names[0] != "Alex" || res.Names[1] != "Robin" {
		t.Fatalf("got %+v", res)
	}
	// Something that isn't Minecraft.
	other, _ := net.Listen("tcp", "127.0.0.1:0")
	defer other.Close()
	go func() {
		if conn, err := other.Accept(); err == nil {
			_, _ = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			conn.Close()
		}
	}()
	if _, err := queryMinecraft(other.Addr().String()); err == nil {
		t.Fatal("a web server passed for Minecraft")
	}
	for _, n := range []int32{0, 1, 127, 128, 255, 25565, 2097151, -1} {
		got, err := readVarint(bytes.NewReader(varint(n)))
		if err != nil || got != n {
			t.Errorf("varint %d: %d %v", n, got, err)
		}
	}
}

// A stand-in Steam server: asks for its challenge back first, as newer servers do, then answers.
func TestQuerySource(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 1400)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			request := buf[:n]
			challenge := []byte{1, 2, 3, 4}
			if !bytes.HasSuffix(request, challenge) {
				_, _ = pc.WriteTo(append([]byte{0xff, 0xff, 0xff, 0xff, 'A'}, challenge...), from)
				continue
			}
			var info bytes.Buffer
			info.Write([]byte{0xff, 0xff, 0xff, 0xff, 'I', 17})
			for _, s := range []string{"Acme's Valheim", "Dashboard", "valheim", "Valheim"} {
				info.WriteString(s)
				info.WriteByte(0)
			}
			_ = binary.Write(&info, binary.LittleEndian, uint16(0))
			info.Write([]byte{3, 10, 0, 'd', 'l', 1, 0})
			_, _ = pc.WriteTo(info.Bytes(), from)
		}
	}()
	res, err := querySource(pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if res.Online != 3 || res.Max != 10 {
		t.Fatalf("got %+v", res)
	}
	if _, err := parseSourceInfo([]byte{17, 'a', 0}); err == nil {
		t.Fatal("a short answer was read")
	}
}
