package alacstream

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"io"
	"math/rand"
	"net"
	"sync/atomic"
	"testing"

	"github.com/itouakirai/mp4ff/mp4"
)

var (
	sampleKey = bytes.Repeat([]byte{0x5a}, 16)
	sampleIV  = bytes.Repeat([]byte{0xd1}, 16)
)

// pipeDecryptor answers [uint32 size][bytes] packets the way the FairPlay
// decryptor does: AES-CBC over the packet from the constant IV. It returns the
// client side of the pipe and a counter of packets served.
func pipeDecryptor(t *testing.T) (*bufio.ReadWriter, *atomic.Int32) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	packets := new(atomic.Int32)
	go func() {
		block, _ := aes.NewCipher(sampleKey)
		for {
			var sz [4]byte
			if _, err := io.ReadFull(server, sz[:]); err != nil {
				return
			}
			buf := make([]byte, binary.LittleEndian.Uint32(sz[:]))
			if _, err := io.ReadFull(server, buf); err != nil {
				return
			}
			cipher.NewCBCDecrypter(block, sampleIV).CryptBlocks(buf, buf)
			packets.Add(1)
			if _, err := server.Write(buf); err != nil {
				return
			}
		}
	}()
	return bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), packets
}

func randomBytes(n int, seed int64) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

func encrypt(t *testing.T, sample []byte, subs []mp4.SubSamplePattern, tenc *mp4.TencBox) []byte {
	t.Helper()
	enc := append([]byte(nil), sample...)
	if err := mp4.EncryptSampleCbcs(enc, sampleKey, sampleIV, subs, tenc); err != nil {
		t.Fatal(err)
	}
	return enc
}

// ALAC: whole-sample encryption (pattern 0:0), no subsamples; the tail that is
// not a whole AES block stays clear. One packet per sample, like handle() expects.
func TestCbcsDecryptSampleFullSampleALAC(t *testing.T) {
	tenc := &mp4.TencBox{DefaultConstantIV: sampleIV}
	for _, size := range []int{15, 16, 333, 4096} {
		plain := randomBytes(size, int64(size))
		enc := encrypt(t, plain, nil, tenc)
		rw, packets := pipeDecryptor(t)
		if err := cbcsDecryptSample(enc, rw, nil, tenc); err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if !bytes.Equal(enc, plain) {
			t.Fatalf("size %d: sample did not decrypt to the original", size)
		}
		if want := int32(min(1, size/16)); packets.Load() != want {
			t.Fatalf("size %d: %d packets sent, want %d", size, packets.Load(), want)
		}
	}
}

// Video: 1:9 stripe pattern inside protected subsamples, clear headers between.
func TestCbcsDecryptSampleStripePatternWithSubsamples(t *testing.T) {
	tenc := &mp4.TencBox{DefaultCryptByteBlock: 1, DefaultSkipByteBlock: 9, DefaultConstantIV: sampleIV}
	subs := []mp4.SubSamplePattern{
		{BytesOfClearData: 5, BytesOfProtectedData: 700},
		{BytesOfClearData: 0, BytesOfProtectedData: 0},
		{BytesOfClearData: 33, BytesOfProtectedData: 170},
	}
	plain := randomBytes(5+700+33+170, 7)
	enc := encrypt(t, plain, subs, tenc)
	if bytes.Equal(enc, plain) {
		t.Fatal("setup: encryption changed nothing")
	}
	rw, _ := pipeDecryptor(t)
	if err := cbcsDecryptSample(enc, rw, subs, tenc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enc, plain) {
		t.Fatal("striped subsamples did not decrypt to the original")
	}
}

func TestCbcsDecryptSamplesDecryptsEverySample(t *testing.T) {
	tenc := &mp4.TencBox{DefaultConstantIV: sampleIV}
	var samples []mp4.FullSample
	var plains [][]byte
	for i := 0; i < 5; i++ {
		p := randomBytes(100+37*i, int64(i))
		plains = append(plains, p)
		samples = append(samples, mp4.FullSample{Data: encrypt(t, p, nil, tenc)})
	}
	rw, _ := pipeDecryptor(t)
	if err := cbcsDecryptSamples(samples, rw, tenc, &mp4.SencBox{}); err != nil {
		t.Fatal(err)
	}
	for i := range samples {
		if !bytes.Equal(samples[i].Data, plains[i]) {
			t.Fatalf("sample %d did not decrypt", i)
		}
	}
}

func TestSwitchKeysAndSendStringWireFormat(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := SendString(w, "0"); err != nil {
		t.Fatal(err)
	}
	if err := SwitchKeys(bufio.NewReadWriter(nil, w)); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if want := []byte{1, '0', 0, 0, 0, 0}; !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("bytes = %v, want %v", buf.Bytes(), want)
	}
}
