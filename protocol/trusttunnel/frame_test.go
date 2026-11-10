package trusttunnel

import (
	"bytes"
	"net/netip"
	"testing"
)

// Byte layouts are written out by hand from PROTOCOL.md section 6, not derived from the codec.
func TestAppendClientFrameLayout(t *testing.T) {
	frame := udpFrame{
		source:      netip.MustParseAddrPort("198.18.0.1:4000"),
		destination: netip.MustParseAddrPort("8.8.4.4:53"),
		payload:     []byte{0xAA, 0xBB},
	}
	got := appendClientFrame(nil, frame, "app")
	want := []byte{
		0, 0, 0, 42, // 37 + len("app") + 2 payload bytes
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 198, 18, 0, 1, 0x0F, 0xA0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 8, 8, 4, 4, 0, 53,
		3, 'a', 'p', 'p',
		0xAA, 0xBB,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("client frame\n got %v\nwant %v", got, want)
	}
}

func TestReadServerFrame(t *testing.T) {
	cases := []struct {
		name        string
		raw         []byte
		source      string
		destination string
		payload     string
	}{
		{
			name: "ipv4",
			raw: append([]byte{0, 0, 0, 38,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 53,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 198, 18, 0, 1, 0x0F, 0xA0}, "hi"...),
			source:      "1.1.1.1:53",
			destination: "198.18.0.1:4000",
			payload:     "hi",
		},
		{
			name: "ipv6",
			raw: append([]byte{0, 0, 0, 37,
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0x01, 0xBB,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 198, 18, 0, 1, 0x0F, 0xA0}, "x"...),
			source:      "[2001:db8::1]:443",
			destination: "198.18.0.1:4000",
			payload:     "x",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frame, err := readServerFrame(bytes.NewReader(c.raw))
			if err != nil {
				t.Fatal(err)
			}
			if frame.source.String() != c.source || frame.destination.String() != c.destination || string(frame.payload) != c.payload {
				t.Fatalf("got %s -> %s %q", frame.source, frame.destination, frame.payload)
			}
		})
	}
}

func TestReadServerFrameRejectsShortLength(t *testing.T) {
	_, err := readServerFrame(bytes.NewReader([]byte{0, 0, 0, 35, 0}))
	if err == nil {
		t.Fatal("expected error for a frame shorter than the fixed header")
	}
}
