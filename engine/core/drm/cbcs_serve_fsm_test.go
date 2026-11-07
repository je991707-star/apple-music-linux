package drm

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"net"
	"testing"
	"time"
)

// pipeConn serves a fixed byte stream to serveCBCS and records what it writes back.
type pipeConn struct {
	net.Conn
	r io.Reader
	w bytes.Buffer
}

func (c *pipeConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *pipeConn) Write(p []byte) (int, error) { return c.w.Write(p) }

func rec(adam, uri string, samples ...[]byte) []byte {
	var b bytes.Buffer
	b.WriteByte(byte(len(adam)))
	b.WriteString(adam)
	b.WriteByte(byte(len(uri)))
	b.WriteString(uri)
	for _, s := range samples {
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(len(s)))
		b.Write(n[:])
		b.Write(s)
	}
	b.Write([]byte{0, 0, 0, 0}) // size 0: end of this key
	return b.Bytes()
}

func run(t *testing.T, in []byte, openKey func(string, string) (func([]byte) error, error)) (*pipeConn, error) {
	t.Helper()
	c := &pipeConn{r: bytes.NewReader(in)}
	done := make(chan error, 1)
	go func() { done <- serveCBCS(context.Background(), c, openKey) }()
	select {
	case err := <-done:
		return c, err
	case <-time.After(3 * time.Second):
		t.Fatalf("serveCBCS did not terminate on %d input bytes", len(in))
		return nil, nil
	}
}

func xorKey(adam, uri string) (func([]byte) error, error) {
	return func(s []byte) error {
		for i := range s {
			s[i] ^= 0xFF
		}
		return nil
	}, nil
}

func TestCBCSKeySwitchKeepsTheConnectionAndUsesEachKey(t *testing.T) {
	var opened []string
	in := append(rec("111", "uri-a", []byte{1, 2, 3}), rec("222", "uri-b", []byte{9})...)
	c, err := run(t, in, func(adam, uri string) (func([]byte) error, error) {
		opened = append(opened, adam)
		return xorKey(adam, uri)
	})
	if err != nil {
		t.Fatalf("clean stream ended with %v", err)
	}
	if len(opened) != 2 || opened[0] != "111" || opened[1] != "222" {
		t.Fatalf("keys opened = %v, want [111 222]", opened)
	}
	if !bytes.Equal(c.w.Bytes(), []byte{0xFE, 0xFD, 0xFC, 0xF6}) {
		t.Fatalf("output = % x", c.w.Bytes())
	}
}

func TestCBCSEmptyAdamIDEndsTheSession(t *testing.T) {
	c, err := run(t, []byte{0}, xorKey)
	if err != nil || c.w.Len() != 0 {
		t.Fatalf("err=%v wrote=%d", err, c.w.Len())
	}
}

// EOF is a clean end only at a record boundary: where the next adamID would start, or where the next
// sample's size header would. Stopping anywhere else is a truncated record and must be an error.
func TestCBCSTruncatedInputIsAnErrorNotACleanEnd(t *testing.T) {
	good := rec("111", "uri", []byte{1, 2, 3, 4})
	const afterHeader, afterSample = 8, 16 // offsets of the two legitimate boundaries inside this record
	for cut := 1; cut < len(good); cut++ {
		_, err := run(t, good[:cut], xorKey)
		legal := cut == afterHeader || cut == afterSample
		if legal && err != nil {
			t.Errorf("prefix %d/%d is a record boundary but failed: %v", cut, len(good), err)
		}
		if !legal && err == nil {
			t.Errorf("prefix %d/%d stops mid-record yet ended cleanly", cut, len(good))
		}
	}
	if _, err := run(t, nil, xorKey); err != nil {
		t.Errorf("an empty stream is a clean end, got %v", err)
	}
	if _, err := run(t, good, xorKey); err != nil {
		t.Errorf("the complete record failed: %v", err)
	}
}

func TestCBCSOpenKeyFailureEndsTheSession(t *testing.T) {
	boom := errors.New("no key")
	_, err := run(t, rec("1", "u", []byte{1}), func(string, string) (func([]byte) error, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the open error", err)
	}
}

func TestCBCSOversizedSampleIsRefusedBeforeAllocating(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{1, 'a', 1, 'u'})
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], 0xFFFFFFFF)
	b.Write(n[:])
	if _, err := run(t, b.Bytes(), xorKey); err == nil {
		t.Fatal("a 4 GiB sample header must be refused")
	}
}

// Whatever bytes arrive, the automaton halts without panicking.
func TestCBCSNeverPanicsOrHangsOnRandomBytes(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 3000; i++ {
		in := make([]byte, rng.Intn(300))
		rng.Read(in)
		if i%3 == 0 && len(in) > 8 { // bias towards plausible headers so deeper states get exercised
			in[0] = byte(1 + rng.Intn(3))
		}
		run(t, in, xorKey)
	}
}
