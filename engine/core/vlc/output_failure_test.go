//go:build vlc_ordering_test && cgo && linux

package vlc

import (
	"encoding/json"
	"engine/internal/playbackcmd"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// This test is allowed only in an unshare-all capsule with a private audio
// server. Kill targets are validated private children, never host processes.
func TestPrivateOutputFailureBoundary(t *testing.T) {
	if os.Getenv("U2_PRIVATE_FAILURE_CAPSULE") != "1" {
		t.Skip("private capsule only")
	}
	kind := os.Getenv("U2_FAILURE_CASE")
	if os.Getenv("PULSE_SERVER") != "unix:/private/run/pulse/native" {
		t.Fatal("private server required")
	}
	pid, e := strconv.Atoi(os.Getenv("U2_PRIVATE_PULSE_PID"))
	if e != nil || pid < 2 {
		t.Fatal("private pid")
	}
	comm, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if e != nil || strings.TrimSpace(string(comm)) != "pipewire-pulse" {
		t.Fatal("private process identity")
	}
	kill := func() {
		if e := syscall.Kill(pid, syscall.SIGTERM); e != nil {
			t.Fatal(e)
		}
		end := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(end) {
			if exec.Command("pactl", "info").Run() != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("private server termination not observed")
	}
	p, e := NewPrivatePulseTestPlayer()
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if kind == "absent-before" {
		kill()
	}
	if e = p.Load("file://" + os.Getenv("ADAM_VLC_TEST_FIXTURE")); e != nil {
		t.Fatal(e)
	}
	// Parsing may progress even if there is no output; do not call Playing proof
	// of a device. All C entry/cleanup is bounded by the external capsule watchdog.
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		_, _, s := p.Time()
		if s == "playing" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	lease := playbackcmd.NewLease("private-boot", "private-A", 1)
	p.mu.Lock()
	p.lease = lease
	p.mu.Unlock()
	if kind == "normal" {
		for _, v := range []int{80, 100} {
			if e = p.SetVolumeForLease(v, lease, func() bool { return true }); e != nil {
				t.Fatal(e)
			}
		}
	}
	var calls atomic.Int32
	current := func() bool {
		n := calls.Add(1)
		if kind == "after-setter-before-getter" && n == 3 {
			kill()
		}
		return true
	}
	begin := time.Now()
	err := p.SetVolumeForLease(80, lease, current)
	elapsed := time.Since(begin)
	if kind == "after-getter" {
		kill()
	}
	if kind == "sink-disappears" {
		out, e := exec.Command("pactl", "-f", "json", "list", "sink-inputs").Output()
		if e != nil {
			t.Fatal(e)
		}
		var inputs []struct {
			Index      int               `json:"index"`
			Properties map[string]string `json:"properties"`
		}
		if json.Unmarshal(out, &inputs) != nil {
			t.Fatal("inputs")
		}
		found := false
		for _, i := range inputs {
			if i.Properties["application.process.id"] == strconv.Itoa(os.Getpid()) {
				found = true
				if e = exec.Command("pw-cli", "destroy", i.Properties["object.id"]).Run(); e != nil {
					t.Fatal(e)
				}
			}
		}
		if !found {
			t.Fatal("own sink not found")
		}
		end := time.Now().Add(200 * time.Millisecond)
		for {
			out, e := exec.Command("pactl", "-f", "json", "list", "sink-inputs").Output()
			if e != nil {
				t.Fatal(e)
			}
			var remaining []struct {
				Properties map[string]string `json:"properties"`
			}
			if json.Unmarshal(out, &remaining) != nil {
				t.Fatal("remaining")
			}
			still := false
			for _, i := range remaining {
				if i.Properties["application.process.id"] == strconv.Itoa(os.Getpid()) {
					still = true
				}
			}
			if !still {
				break
			}
			if time.Now().After(end) {
				t.Fatal("sink input disappearance not confirmed")
			}
			time.Sleep(time.Millisecond)
		}
	}
	status := "native-confirmed"
	if err != nil {
		status = "unconfirmed"
	}
	stage := "native-getter-confirmed"
	if err != nil {
		stage = "unconfirmed"
	}
	// A matching getter after server loss is allowed as a narrow native result,
	// never promoted to a current device/sample/audibility capability.
	raw := p.PrivateRawVolumeReadback()
	row := map[string]any{"case": kind, "status": status, "requestedVolume": 80, "nativeReadback": raw, "outputBackend": p.OutputBackend(), "confirmationStage": stage, "mayHaveApplied": true, "sourceGeneration": lease.Identity.Generation, "leaseIdentity": lease.Identity, "elapsedMs": elapsed.Milliseconds(), "outputCapability": "unverified", "currentChecks": calls.Load(), "backendClaim": "explicit-test-selector, not proof of active output"}
	b, _ := json.Marshal(row)
	t.Log(string(b))
	if err != nil && !errors.Is(err, ErrVolumeUnconfirmed) {
		t.Fatal(err)
	}
	if elapsed > 400*time.Millisecond {
		t.Fatal("250ms operation plus measured termination overhead exceeded; no timeout enlargement")
	}
}
