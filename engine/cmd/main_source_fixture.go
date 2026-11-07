//go:build u2_source_fixture && vlc_ordering_test && linux && cgo

package main

// This data source is excluded from production. It cannot authenticate an account.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"engine/core/media"
	"engine/core/pipeline"
	"engine/core/playback"
	"engine/core/vlc"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func mainSourceFactoryKind() string { return "u2-fixed-synthetic-v1" }

type fixedMainProvider struct{}

func mainSourceProvider(media.Provider) media.Provider { return fixedMainProvider{} } // test build fails closed for ALL account assets
var fixedMainFixtures = map[string]struct {
	file, reference string
	duration        int
}{
	"alac-a":          {"fragmented.mp4", "reference.s16le", 4000},
	"alac-b":          {"b-distinct.m4a", "b-distinct.s16le", 4000},
	"alac-tail":       {"silence.m4a", "silence.s16le", 4500},
	"alac-fail":       {"recovery-12s.m4a", "recovery-12s.s16le", 12000},
	"alac-fail-short": {"fragmented.mp4", "reference.s16le", 4000},
	"alac-delay":      {"fragmented.mp4", "reference.s16le", 4000},
	"alac-404":        {"fragmented.mp4", "reference.s16le", 4000},
}

var fixedMainHashes = map[string]string{
	"b-distinct.s16le":   "02e10bff81e3d2d06f4130476d4c30fbe2500a892c66b5df1c67b84231786ed4",
	"b-distinct.m4a":     "84f13ec22e04f6dced13b1c59fe2a0a43892ff9198e0fa5c4270be0232552535",
	"recovery-12s.s16le": "316c85ddc200dc5a51436a50e4d5230bae1ba12c06bed1fe193a07a5d2fe89e9",
	"recovery-12s.m4a":   "0cea5157417fe97a921f9fad85989fab8fcd3051c4a8fe73668a34fb63376a66",
	"fragmented.mp4":     "9891800dc42f728ea12f75c9e11aee3eebb76236cd8714a8b6fe6d970dddc12d",
	"reference.s16le":    "4502fb87e6c2651a3c5247310a187ce294f2032b37ad93d04bc3dc679bb50e36",
	"silence.m4a":        "a08f2439b82526df9a0df501886c93830ed4897889ba6dab88665bbedc6d3685",
	"silence.s16le":      "693944b408abe7692fa5267920a94fc1e6bacfe43bb3c3227efa7677ae332d5c",
}

func pinnedMainBytes(root, name string) ([]byte, error) {
	path := filepath.Join(root, name)
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || resolved != path {
		return nil, errors.New("fixture symlink denied")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != fixedMainHashes[name] {
		return nil, errors.New("fixture hash mismatch")
	}
	return data, nil
}
func fixtureBytes(intent string) ([]byte, []byte, int, error) {
	f, ok := fixedMainFixtures[intent]
	if !ok {
		return nil, nil, 0, errors.New("fixed fixture required")
	}
	root := os.Getenv("ADAM_MAIN_FIXTURE_DIR")
	canonical, e := filepath.EvalSymlinks(root)
	if e != nil || root != canonical || !filepath.IsAbs(root) {
		return nil, nil, 0, errors.New("private fixture directory required")
	}
	// Paths are fixed constants, never supplied through Renderer or HTTP.
	data, e := pinnedMainBytes(root, f.file)
	if e != nil {
		return nil, nil, 0, e
	}
	ref, e := pinnedMainBytes(root, f.reference)
	if e != nil {
		return nil, nil, 0, e
	}
	if len(ref) != f.duration*48*4 || len(data) > 8<<20 {
		return nil, nil, 0, errors.New("fixture size mismatch")
	}
	return data, ref, f.duration, nil
}
func (fixedMainProvider) Open(ctx context.Context, q media.OpenRequest) (*media.Session, error) {
	data, _, duration, e := fixtureBytes(q.AssetID)
	if e != nil {
		return nil, e
	}
	if q.Token != "" || q.MUT != "" {
		return nil, errors.New("account credentials forbidden")
	}
	return &media.Session{Kind: "song", Metadata: media.Metadata{Title: "Offline synthetic " + q.AssetID, ArtistName: "Local fixture", DurationMs: duration}, Tracks: []media.Track{{Kind: pipeline.KindAudio, Codec: pipeline.CodecALAC, SampleRate: 48000, BitDepth: 16, ChannelCount: 2, Open: func(context.Context) (*pipeline.Stream, error) {
		return &pipeline.Stream{Source: &fixedMainSource{data: data, mode: q.AssetID}, Kind: pipeline.KindAudio, Codec: pipeline.CodecALAC, DurationMs: duration}, nil
	}}}}, nil
}

type fixedMainSource struct {
	data []byte
	mode string
}

func (f *fixedMainSource) Stream(ctx context.Context, w io.Writer) error {
	if f.mode == "alac-delay" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	for p := 0; p < len(f.data); {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		end := p + 4096
		if end > len(f.data) {
			end = len(f.data)
		}
		if _, e := io.Copy(w, bytes.NewReader(f.data[p:end])); e != nil {
			return e
		}
		p = end
		if (f.mode == "alac-fail" || f.mode == "alac-fail-short") && p >= len(f.data)/4 {
			return errors.New("controlled offline source failure")
		}
	}
	return nil
}
func openMainTypedSource(ctx context.Context, m *playback.Manager, intent string) (*playback.Session, error) {
	if _, ok := fixedMainFixtures[intent]; !ok {
		return nil, errors.New("fixed fixture required")
	}
	sess, e := m.Open(ctx, playback.OpenRequest{AssetID: intent, Private: true, Lossless: true})
	if e == nil {
		_, ref, _, err := fixtureBytes(intent)
		if err != nil {
			m.Release(sess.ID)
			return nil, err
		}
		mainFixtureReferences.Store(sess.ID, ref)
		if intent == "alac-404" {
			go func() { <-time.After(2 * time.Second); m.Release(sess.ID) }()
		}
	}
	return sess, e
}

var mainFixtureReferences sync.Map
var mainFixtureSinks sync.Map

func mainSourcePlayer() (*vlc.Player, error) {
	if mode := os.Getenv("ADAM_MAIN_PRIVATE_OUTPUT"); mode == "pulse" || mode == "auto" {
		if !strings.HasPrefix(os.Getenv("PULSE_SERVER"), "unix:/private/run/") {
			return nil, errors.New("private pulse endpoint required")
		}
		if mode == "auto" {
			return vlc.New()
		}
		return vlc.NewPrivatePulseTestPlayer()
	}

	p, s, e := vlc.NewIsolatedPlayer()
	if e == nil {
		mainFixtureSinks.Store(p, s)
	}
	return p, e
}
func mainSourceDiagnostics(s *APIServer) map[string]any {
	if v, ok := mainFixtureSinks.Load(s.vlcPlayer); ok {
		before := s.vlcPlayer.OrderingOwnership()
		result := v.(*vlc.IsolatedSink).Snapshot()
		after := s.vlcPlayer.OrderingOwnership()
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(after)
		result["native"] = after
		result["coherent"] = bytes.Equal(a, b)
		result["resources"] = mainFixtureResources()
		return result
	}
	if s.vlcPlayer.OutputBackend() == "native-pulse" || os.Getenv("ADAM_MAIN_PRIVATE_OUTPUT") == "auto" {
		v, _ := s.vlcPlayer.Volume()
		return map[string]any{"native": s.vlcPlayer.OrderingOwnership(), "outputBackend": s.vlcPlayer.OutputBackend(), "nativeVolume": v, "resources": mainFixtureResources()}
	}
	return nil
}
func mainFixtureResources() map[string]any {
	f, _ := os.ReadDir("/proc/self/fd")
	b, _ := os.ReadFile("/proc/self/status")
	rss := int64(0)
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				rss, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]any{"fd": len(f), "goroutines": runtime.NumGoroutine(), "rssKiB": rss, "heapAlloc": m.HeapAlloc, "heapInuse": m.HeapInuse, "totalAlloc": m.TotalAlloc, "numGC": m.NumGC}
}

func prepareMainSourceDiagnostics(s *APIServer, id string) {
	if v, ok := mainFixtureSinks.Load(s.vlcPlayer); ok {
		if ref, found := mainFixtureReferences.Load(id); found {
			s.mainSources.mu.Lock()
			g := s.mainSources.grants[id]
			var binding map[string]any
			if g != nil {
				binding = map[string]any{"sessionId": g.Session, "assetId": g.Asset, "sourceGeneration": g.SourceGeneration, "leaseId": g.LeaseID, "commandSequence": g.sequence}
			}
			s.mainSources.mu.Unlock()
			v.(*vlc.IsolatedSink).ResetForSource(ref.([]byte), binding)
		}
	}
}

func disposeMainSourceDiagnostics(_ *APIServer, id string) { mainFixtureReferences.Delete(id) }
func closeMainSourceDiagnostics(s *APIServer) {
	if v, ok := mainFixtureSinks.LoadAndDelete(s.vlcPlayer); ok {
		v.(*vlc.IsolatedSink).Close()
	}
}

func mainSourceAccountNetworkAllowed() bool { return false }

func mainScopedLibrary(page, limit int) (any, bool) {
	all := []map[string]any{{"id": "alac-a", "title": "Fixed synthetic A", "durationMs": 4000}, {"id": "alac-b", "title": "Fixed synthetic B", "durationMs": 4000}}
	start := page * limit
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	return map[string]any{"kind": "public", "page": page, "items": all[start:end], "hasMore": end < len(all)}, true
}
func mainScopedArtwork() (any, bool) {
	return map[string]any{"mime": "image/png", "base64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j9ioAAAAASUVORK5CYII="}, true
}
