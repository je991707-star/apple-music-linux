package main

import (
	"bytes"
	"encoding/json"
	"engine/core/drm"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type privateAuthGuard struct {
	parentPID   int
	parentStart string
	peerCheck   func(*http.Request) error // only _test.go injects this; production uses kernel/proc metadata.
}

func procStart(pid int) (string, error) {
	data, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return "", errors.New("parent metadata unavailable")
	}
	f := strings.Fields(string(data[i+1:]))
	if len(f) <= 19 {
		return "", errors.New("parent metadata unavailable")
	}
	return f[19], nil
}
func newPrivateAuthGuard() privateAuthGuard {
	p := os.Getppid()
	start, _ := procStart(p)
	return privateAuthGuard{parentPID: p, parentStart: start}
}
func (g privateAuthGuard) checkPeer(r *http.Request) error {
	if g.peerCheck != nil {
		return g.peerCheck(r)
	}
	if runtime.GOOS != "linux" || g.parentPID <= 0 || g.parentStart == "" || os.Getppid() != g.parentPID {
		return errors.New("private Main parent unavailable")
	}
	start, e := procStart(g.parentPID)
	if e != nil || start != g.parentStart {
		return errors.New("private Main parent changed")
	}
	remote, e := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	if e != nil {
		return e
	}
	local, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
	if !ok {
		return errors.New("private TCP identity unavailable")
	}
	inode, e := privateSocketInode(procNetPaths, procAddr(remote), procAddr(local), uint32(os.Geteuid()))
	if e != nil {
		return e
	}
	entries, e := os.ReadDir(fmt.Sprintf("/proc/%d/fd", g.parentPID))
	if e != nil {
		return e
	}
	for _, fd := range entries {
		target, e := os.Readlink(filepath.Join(fmt.Sprintf("/proc/%d/fd", g.parentPID), fd.Name()))
		if e == nil && target == "socket:["+inode+"]" {
			return nil
		}
	}
	return errors.New("connection is not owned by Main parent")
}
func privateSocketInode(paths []string, local, remote string, uid uint32) (string, error) {
	for _, p := range paths {
		data, e := os.ReadFile(p)
		if e != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || f[1] != local || f[2] != remote {
				continue
			}
			got, e := strconv.ParseUint(f[7], 10, 32)
			if e != nil || uint32(got) != uid {
				continue
			}
			if _, e = strconv.ParseUint(f[9], 10, 64); e == nil && f[9] != "0" {
				return f[9], nil
			}
		}
	}
	return "", errors.New("client socket identity unavailable")
}

type privateAuthRequest struct {
	Operation   string            `json:"operation"`
	Binding     drm.AuthBinding   `json:"binding"`
	Input       *drm.PrivateInput `json:"input,omitempty"`
	ChallengeID string            `json:"challengeId,omitempty"`
}

func (s *APIServer) handlePrivateAuth(w http.ResponseWriter, r *http.Request) {
	reject := func() { http.Error(w, "private authentication rejected", http.StatusForbidden) }
	if os.Getenv("ADAM_MAIN_PLAYBACK") != "1" || r.TLS == nil || !r.TLS.HandshakeComplete || r.Method != "POST" || r.URL.Path != "/api/v1/main/auth" || r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" || !sameProof(r.Header.Get("X-Api-Token"), os.Getenv("ADAM_API_TOKEN")) || !sameProof(r.Header.Get("X-Main-Issuer"), os.Getenv("ADAM_MAIN_SOURCE_ISSUER")) {
		reject()
		return
	}
	if s.privateAuth.checkPeer(r) != nil {
		reject()
		return
	}
	var q privateAuthRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, readErr := io.ReadAll(r.Body)
	defer clear(raw)
	if readErr != nil {
		http.Error(w, "invalid private authentication request", 400)
		return
	}
	defer func() { q.Input.Clear() }()
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid private authentication request", 400)
		return
	}
	epoch, e := strconv.ParseUint(os.Getenv("ADAM_MAIN_AUTH_EPOCH"), 10, 64)
	if e != nil || epoch == 0 || q.Binding.AccountEpoch != epoch || !sourceUUID.MatchString(os.Getenv("ADAM_MAIN_AUTH_ACCOUNT")) || q.Binding.AccountID != os.Getenv("ADAM_MAIN_AUTH_ACCOUNT") || !sourceUUID.MatchString(os.Getenv("ADAM_MAIN_AUTH_OWNER")) || q.Binding.EngineOwnerID != os.Getenv("ADAM_MAIN_AUTH_OWNER") || q.Binding.BootNonce != os.Getenv("ADAM_MAIN_BOOT_NONCE") || q.Binding.EngineBoot != s.vlcGate.order.Snapshot().Boot {
		reject()
		return
	}
	if s.dm == nil {
		http.Error(w, "private authentication unavailable", 503)
		return
	}
	if q.Operation != "credentials" && q.Operation != "challenge" && (q.Input != nil || q.ChallengeID != "") {
		http.Error(w, "unexpected private input", 400)
		return
	}
	var snap drm.DRMSnapshot
	var inputStatus drm.PrivateInputStatus
	var err error
	code := 200
	switch q.Operation {
	case "credentials", "challenge":
		if q.Input == nil {
			http.Error(w, "missing private input", 400)
			return
		}
		inputStatus, err = s.dm.HandoffPrivateInput(r.Context(), q.Binding, q.Operation, q.ChallengeID, q.Input)
		if errors.Is(err, drm.ErrPrivateInputUnavailable) {
			http.Error(w, "private credential consumer unavailable", 503)
			return
		}
		snap = s.dm.Status()
	case "begin":
		snap, err = s.dm.BeginPrivateIntent(r.Context(), q.Binding)
		code = 202
	case "status":
		snap, err = s.dm.PrivateIntentStatus(r.Context(), q.Binding, true)
	case "cancel":
		snap, err = s.dm.EndPrivateIntent(r.Context(), q.Binding, drm.AuthCancelled)
	case "revoke":
		snap, err = s.dm.EndPrivateIntent(r.Context(), q.Binding, drm.AuthRevoked)
	default:
		http.Error(w, "unsupported private authentication operation", 400)
		return
	}
	if err != nil {
		http.Error(w, "private authentication scope unavailable", 409)
		return
	}
	// This protocol is progress-only. It cannot transport a completion grant or a
	// credential. U218's qualified completion seam remains separate and unavailable.
	snap.NativeAuthenticated = false
	snap.Capabilities = drm.CapabilityState{}
	if snap.State.Session == drm.SessionValid {
		snap.State.Session = drm.SessionEmpty
	}
	snap.AuthEvidence = nil
	if snap.State.Authentication == drm.AuthLoggedIn {
		snap.State.Authentication = drm.AuthLoggingIn
	}
	snap.Message = "private intent only; Native login and completion remain unavailable"
	if q.Operation == "status" {
		inputStatus, _ = s.dm.PrivateInputStatus(r.Context(), q.Binding)
	}
	writeJSON(w, code, struct {
		drm.DRMSnapshot
		drm.PrivateInputStatus
	}{snap, inputStatus})
}
