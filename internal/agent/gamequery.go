package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// Who's playing: the agent asks a running game in the game's own query protocol, at the container's own
// address (so the query port needn't be opened to the world). Minecraft: Java Edition answers the server
// list ping; games on Steam answer A2S_INFO. A game that doesn't answer just has no player count.

const queryTimeout = 3 * time.Second

func queryGame(ctx context.Context, a protocol.GameQueryArgs) (*protocol.GameQueryResult, error) {
	if a.ID <= 0 || len(a.Ports) == 0 || len(a.Ports) > 4 {
		return nil, errors.New("that isn't a game to ask")
	}
	// The container's address on Docker's network; the server's own if that can't be found.
	host := "127.0.0.1"
	if out, err := docker(ctx, "", "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", gameContainer(a.ID)); err == nil {
		if ips := strings.Fields(out); len(ips) > 0 && net.ParseIP(ips[0]) != nil {
			host = ips[0]
		}
	}
	var last error
	for _, port := range a.Ports {
		if port < 1 || port > 65535 {
			continue
		}
		address := net.JoinHostPort(host, strconv.Itoa(port))
		var res *protocol.GameQueryResult
		var err error
		switch a.Kind {
		case "minecraft":
			res, err = queryMinecraft(address)
		case "source":
			res, err = querySource(address)
		default:
			return nil, errors.New("unknown query " + a.Kind)
		}
		if err == nil {
			return res, nil
		}
		last = err
	}
	return nil, last
}

// queryMinecraft is the server list ping: a handshake asking for status, then the status as JSON.
func queryMinecraft(address string) (*protocol.GameQueryResult, error) {
	conn, err := net.DialTimeout("tcp", address, queryTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(queryTimeout))
	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)

	var handshake bytes.Buffer
	handshake.WriteByte(0x00)                 // packet: handshake
	handshake.Write(varint(-1))               // protocol version: any
	handshake.Write(varint(int32(len(host)))) // the address it was reached at
	handshake.WriteString(host)               //
	_ = binary.Write(&handshake, binary.BigEndian, uint16(port))
	handshake.Write(varint(1)) // next: status
	if _, err := conn.Write(append(varint(int32(handshake.Len())), handshake.Bytes()...)); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil { // packet: status request
		return nil, err
	}

	r := bufio.NewReader(conn)
	if _, err := readVarint(r); err != nil { // the packet's length
		return nil, err
	}
	if id, err := readVarint(r); err != nil || id != 0 {
		return nil, errors.New("not a Minecraft status")
	}
	size, err := readVarint(r)
	if err != nil || size < 2 || size > 1<<20 {
		return nil, errors.New("not a Minecraft status")
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, err
	}
	var status struct {
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
			Sample []struct {
				Name string `json:"name"`
			} `json:"sample"`
		} `json:"players"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, errors.New("not a Minecraft status")
	}
	res := &protocol.GameQueryResult{Online: status.Players.Online, Max: status.Players.Max}
	for _, p := range status.Players.Sample {
		if name := cleanName(p.Name); name != "" && len(res.Names) < 50 {
			res.Names = append(res.Names, name)
		}
	}
	return res, nil
}

// varint is a number as Minecraft's protocol writes it: 7 bits a byte, the top bit saying there's more.
func varint(n int32) []byte {
	u := uint32(n)
	var out []byte
	for {
		b := byte(u & 0x7f)
		u >>= 7
		if u != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

func readVarint(r io.ByteReader) (int32, error) {
	var n uint32
	for shift := uint(0); shift < 35; shift += 7 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		n |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return int32(n), nil
		}
	}
	return 0, errors.New("a number too long")
}

// querySource is Steam's A2S_INFO over UDP. Servers answer straight away, or with a challenge to send back.
func querySource(address string) (*protocol.GameQueryResult, error) {
	conn, err := net.DialTimeout("udp", address, queryTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(queryTimeout))
	request := append([]byte{0xff, 0xff, 0xff, 0xff, 'T'}, "Source Engine Query\x00"...)
	buf := make([]byte, 1400)
	send := request
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := conn.Write(send); err != nil {
			return nil, err
		}
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if n < 5 || !bytes.Equal(buf[:4], []byte{0xff, 0xff, 0xff, 0xff}) {
			return nil, errors.New("not a Steam server's answer")
		}
		switch buf[4] {
		case 'A': // a challenge: ask again with it
			if n < 9 {
				return nil, errors.New("not a Steam server's answer")
			}
			send = append(append([]byte{}, request...), buf[5:9]...)
		case 'I':
			return parseSourceInfo(buf[5:n])
		default:
			return nil, errors.New("not a Steam server's answer")
		}
	}
	return nil, errors.New("the server kept asking")
}

// parseSourceInfo reads A2S_INFO's answer: protocol, name, map, folder, game (each text ends with a zero
// byte), the app's ID (2 bytes), then players and the most there can be.
func parseSourceInfo(b []byte) (*protocol.GameQueryResult, error) {
	if len(b) < 1 {
		return nil, errors.New("a short answer")
	}
	b = b[1:] // protocol
	for i := 0; i < 4; i++ {
		end := bytes.IndexByte(b, 0)
		if end < 0 {
			return nil, errors.New("a short answer")
		}
		b = b[end+1:]
	}
	if len(b) < 4 {
		return nil, errors.New("a short answer")
	}
	return &protocol.GameQueryResult{Online: int(b[2]), Max: int(b[3])}, nil
}

// cleanName keeps a player's name to what a name can be, as it's shown in the panel.
func cleanName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= ' ' && r != 0x7f && b.Len() < 40 {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
