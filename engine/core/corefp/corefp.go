// Package corefp loads CoreFP.dll (Apple's FairPlay library from the Windows
// Apple Music app) via a Wine subprocess and exposes SPC generation, CKC
// processing, and sample decryption.
//
// The user places CoreFP.dll at ~/.config/adam/CoreFP.dll (BYOL).  The Wine
// helper binary (corefp_helper.exe.so) must be next to the engine binary or
// in the directory given by ADAM_COREFP_HELPER.
//
// Protocol: stdin/stdout, length-prefixed frames.
//   [1 byte op][4 bytes payload-len LE][payload]
//   response: [1 byte status (0=ok,1=err)][4 bytes len][payload]
package corefp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// ── opcodes ────────────────────────────────────────────────────────────────

const (
	opInit       = 0x01
	opGenSPC     = 0x02
	opProcessCKC = 0x03
	opDecrypt    = 0x04
	opShutdown   = 0xFF

	statusOK  = 0x00
	statusErr = 0x01
)

// ── public types ──────────────────────────────────────────────────────────

// KeyCtx is an opaque 64-bit handle returned by ProcessCKC and consumed by
// Decrypt.  It represents a key context inside the Wine helper process.
type KeyCtx uint64

// ── package-level singleton ───────────────────────────────────────────────

var (
	mu      sync.Mutex
	helper  *helperProc
	initErr error
	once    sync.Once
)

// IsAvailable returns true when CoreFP.dll is present at the BYOL path AND
// the Wine helper binary exists.  It does not start the helper.
func IsAvailable() bool {
	_, err1 := dllPath()
	_, err2 := helperBinary()
	return err1 == nil && err2 == nil
}

// Init starts the Wine helper process and initialises CoreFP.dll.  Safe to
// call multiple times; subsequent calls are no-ops.
func Init() error {
	once.Do(func() {
		initErr = start()
	})
	return initErr
}

// GenerateSPC sends device_info to CoreFP and returns the SPC blob and the
// 20-byte KID embedded in the SPC header.
func GenerateSPC(deviceInfo []byte) (spc []byte, kid []byte, err error) {
	if err = Init(); err != nil {
		return nil, nil, err
	}
	resp, err := call(opGenSPC, deviceInfo)
	if err != nil {
		return nil, nil, err
	}
	if len(resp) < 20 {
		return nil, nil, fmt.Errorf("corefp: GEN_SPC response too short (%d bytes)", len(resp))
	}
	return resp[20:], resp[:20], nil
}

// ProcessCKC decodes a Content Key Container received from Apple's license
// server and returns a KeyCtx usable with Decrypt.
func ProcessCKC(ckc []byte) (KeyCtx, error) {
	if err := Init(); err != nil {
		return 0, err
	}
	resp, err := call(opProcessCKC, ckc)
	if err != nil {
		return 0, err
	}
	if len(resp) < 8 {
		return 0, fmt.Errorf("corefp: PROCESS_CKC response too short (%d bytes)", len(resp))
	}
	return KeyCtx(binary.LittleEndian.Uint64(resp[:8])), nil
}

// Decrypt decrypts data in-place using key_ctx.  data must be a multiple of
// 16 bytes (AES block size).  CBCS always uses a zero IV.
func Decrypt(kctx KeyCtx, data []byte) error {
	if err := Init(); err != nil {
		return err
	}
	payload := make([]byte, 8+len(data))
	binary.LittleEndian.PutUint64(payload[:8], uint64(kctx))
	copy(payload[8:], data)

	resp, err := call(opDecrypt, payload)
	if err != nil {
		return err
	}
	if len(resp) != len(data) {
		return fmt.Errorf("corefp: DECRYPT response length mismatch (%d vs %d)", len(resp), len(data))
	}
	copy(data, resp)
	return nil
}

// Shutdown stops the helper process.  Subsequent calls to Init will restart
// it.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if helper != nil {
		_ = helper.send(opShutdown, nil)
		helper.close()
		helper = nil
	}
	once = sync.Once{} // allow re-init
}

// ── internal ──────────────────────────────────────────────────────────────

func start() error {
	dll, err := dllPath()
	if err != nil {
		return err
	}
	bin, err := helperBinary()
	if err != nil {
		return err
	}

	h, err := newHelper(bin)
	if err != nil {
		return fmt.Errorf("corefp: spawn helper: %w", err)
	}

	// Send INIT with the DLL path
	if err := h.send(opInit, []byte(dll)); err != nil {
		h.close()
		return fmt.Errorf("corefp: init send: %w", err)
	}
	if _, err := h.recv(); err != nil {
		h.close()
		return fmt.Errorf("corefp: init response: %w", err)
	}

	mu.Lock()
	helper = h
	mu.Unlock()
	return nil
}

func call(op byte, payload []byte) ([]byte, error) {
	mu.Lock()
	h := helper
	mu.Unlock()
	if h == nil {
		return nil, errors.New("corefp: not initialised")
	}
	if err := h.send(op, payload); err != nil {
		return nil, err
	}
	return h.recv()
}

// dllPath returns the BYOL CoreFP.dll path.
// Linux default: ~/.config/Adam/LibCoreFP/CoreFP.dll
// Windows default: %PROGRAMFILES%\Adam\LibCoreFP\CoreFP.dll
func dllPath() (string, error) {
	if p := os.Getenv("ADAM_COREFP_DLL"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p := defaultDLLPath()
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("corefp: CoreFP.dll not found at %s (set ADAM_COREFP_DLL to override)", p)
	}
	return p, nil
}

// helperBinary finds corefp_helper.exe.so next to the engine binary.
func helperBinary() (string, error) {
	if p := os.Getenv("ADAM_COREFP_HELPER"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	p := filepath.Join(dir, "corefp_helper.exe.so")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("corefp: corefp_helper.exe.so not found next to engine binary (set ADAM_COREFP_HELPER to override)")
	}
	return p, nil
}

// ── helperProc: subprocess + I/O ─────────────────────────────────────────

type helperProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
}

func newHelper(soPath string) (*helperProc, error) {
	wine, err := exec.LookPath("wine")
	if err != nil {
		return nil, fmt.Errorf("corefp: wine not found in PATH: %w", err)
	}
	cmd := exec.Command(wine, soPath)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &helperProc{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

func (h *helperProc) send(op byte, payload []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	buf := make([]byte, 5+len(payload))
	buf[0] = op
	binary.LittleEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	_, err := h.stdin.Write(buf)
	return err
}

func (h *helperProc) recv() ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	hdr := make([]byte, 5)
	if _, err := io.ReadFull(h.stdout, hdr); err != nil {
		return nil, fmt.Errorf("corefp: read response header: %w", err)
	}
	status := hdr[0]
	plen := binary.LittleEndian.Uint32(hdr[1:5])

	var payload []byte
	if plen > 0 {
		payload = make([]byte, plen)
		if _, err := io.ReadFull(h.stdout, payload); err != nil {
			return nil, fmt.Errorf("corefp: read response payload: %w", err)
		}
	}

	if status == statusErr {
		msg := "corefp: helper error"
		if len(payload) > 0 {
			msg = fmt.Sprintf("corefp: %s", string(payload))
		}
		return nil, errors.New(msg)
	}
	return payload, nil
}

func (h *helperProc) close() {
	_ = h.stdin.Close()
	_ = h.stdout.Close()
	_ = h.cmd.Wait()
}
