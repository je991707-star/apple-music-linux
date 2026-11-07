package ffmpeg

import (
	"encoding/binary"
	"testing"
)

func blobHeader(m, n, sr int32) []byte {
	b := make([]byte, 16)
	copy(b, "SOFA")
	binary.LittleEndian.PutUint32(b[4:], uint32(m))
	binary.LittleEndian.PutUint32(b[8:], uint32(n))
	binary.LittleEndian.PutUint32(b[12:], uint32(sr))
	return b
}

func TestParseSOFABlobRejectsHostileDimensions(t *testing.T) {
	for name, hdr := range map[string][]byte{
		"negative M":      blobHeader(-1, 4, 48000),
		"negative N":      blobHeader(4, -1, 48000),
		"zero M":          blobHeader(0, 4, 48000),
		"huge M and N":    blobHeader(1<<30, 1<<30, 48000),
		"overflowing M*N": blobHeader(2147483647, 2147483647, 48000),
		"bad rate":        blobHeader(2, 2, 0),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked instead of returning an error: %v", r)
				}
			}()
			if _, _, err := parseSOFABlob(hdr); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseSOFABlobAcceptsWellFormed(t *testing.T) {
	const m, n = 2, 3
	blob := blobHeader(m, n, 48000)
	blob = append(blob, make([]byte, m*2*n*8+m*2*8)...)
	d, sr, err := parseSOFABlob(blob)
	if err != nil || d == nil || sr != 48000 {
		t.Fatalf("got %v sr=%d err=%v", d, sr, err)
	}
}
