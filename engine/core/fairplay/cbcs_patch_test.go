package fairplay

import (
	"context"
	"testing"
	"time"

	"github.com/itouakirai/mp4ff/mp4"
)

// alacLikeFragment builds a moof shaped like Apple's ALAC fragments: a v0 tfdt
// and nTruns truns of samplesPerTrun samples each, every trun with a data offset.
func alacLikeFragment(t *testing.T, nTruns, samplesPerTrun int, dur uint32) *mp4.Fragment {
	t.Helper()
	frag, err := mp4.CreateFragment(7, 3)
	if err != nil {
		t.Fatal(err)
	}
	traf := frag.Moof.Traf
	traf.Tfdt.Version = 0
	traf.Tfhd.SampleDescriptionIndex = 2
	traf.Tfhd.Flags |= 0x02 // sample-description-index-present
	first := traf.Trun
	for i := 0; i < nTruns; i++ {
		trun := first
		if i > 0 {
			trun = mp4.CreateTrun(uint32(i))
			traf.AddChild(trun)
		}
		trun.DataOffset = int32(1000 + i)
		for s := 0; s < samplesPerTrun; s++ {
			trun.AddSample(mp4.Sample{Dur: dur, Size: 10})
		}
	}
	return frag
}

// ALAC moofs carry 11 truns (10×16 + 1×1 frames). The decode time must advance
// by every trun's duration, not just the first one's, or VLC reports a
// "Fragment sequence discontinuity" about 28 s in.
func TestPatchAlacFragmentSumsEveryTrun(t *testing.T) {
	frag := alacLikeFragment(t, 11, 16, 4096)
	frag.Moof.Traf.Truns[10].Samples = frag.Moof.Traf.Truns[10].Samples[:1]

	next := patchAlacFragment(frag, 1_000_000)
	if want := uint64(1_000_000 + 161*4096); next != want {
		t.Fatalf("next decode time = %d, want %d (all 161 frames)", next, want)
	}
	if got := frag.Moof.Traf.Tfdt.BaseMediaDecodeTime(); got != 1_000_000 {
		t.Fatalf("tfdt = %d, want the accumulated time 1000000", got)
	}
	if skip := skipFragTfdtDelta(alacLikeFragment(t, 11, 16, 4096)); skip != 176*4096 {
		t.Fatalf("skipFragTfdtDelta = %d, want %d", skip, 176*4096)
	}
}

// Upgrading tfdt to 64-bit grows the moof by 4 bytes; every trun's data offset
// must move with it or the samples are read from the wrong place.
func TestPatchAlacFragmentShiftsDataOffsetsWhenTfdtGrows(t *testing.T) {
	frag := alacLikeFragment(t, 3, 2, 1024)
	before := frag.Moof.Size()
	patchAlacFragment(frag, 0)
	grow := int32(frag.Moof.Size() - before)
	if grow != 4 {
		t.Fatalf("moof grew by %d bytes, want 4 (tfdt v0 → v1)", grow)
	}
	if frag.Moof.Traf.Tfdt.Version != 1 {
		t.Fatal("tfdt must be version 1 after patching")
	}
	for i, trun := range frag.Moof.Traf.Truns {
		if want := int32(1000+i) + grow; trun.DataOffset != want {
			t.Errorf("trun %d data offset = %d, want %d", i, trun.DataOffset, want)
		}
	}
}

// Apple's CDN varies TrackID and SampleDescriptionIndex between fragments,
// which makes ffmpeg skip the trun; both are normalised to 1.
func TestPatchAlacFragmentNormalisesTrackAndSequence(t *testing.T) {
	frag := alacLikeFragment(t, 1, 1, 1024)
	patchAlacFragmentSeq(frag, 0, 0)
	tfhd := frag.Moof.Traf.Tfhd
	if tfhd.TrackID != 1 || tfhd.SampleDescriptionIndex != 1 {
		t.Fatalf("tfhd track=%d sdi=%d, want 1/1", tfhd.TrackID, tfhd.SampleDescriptionIndex)
	}
	if seq := frag.Moof.Mfhd.SequenceNumber; seq != 0 {
		t.Fatalf("mfhd sequence = %d, want 0 (VLC counts from 0)", seq)
	}
}

func TestPatchAlacFragmentWithoutMoofIsANoOp(t *testing.T) {
	if got := patchAlacFragment(&mp4.Fragment{}, 42); got != 42 {
		t.Fatalf("got %d, want the unchanged 42", got)
	}
	if got := skipFragTfdtDelta(&mp4.Fragment{}); got != 0 {
		t.Fatalf("skipFragTfdtDelta on an empty fragment = %d", got)
	}
}

func TestPatchMoovDuration(t *testing.T) {
	init := mp4.CreateEmptyInit()
	init.AddEmptyTrack(44100, "audio", "en")
	init.Moov.Mvhd.Timescale = 1000

	patchMoovDuration(init, 0)
	if init.Moov.Mvhd.Duration != 0 {
		t.Fatal("durationMs 0 must leave the moov unchanged")
	}
	patchMoovDuration(init, 298_144)
	if init.Moov.Mvhd.Duration != 298_144 || init.Moov.Trak.Tkhd.Duration != 298_144 {
		t.Fatalf("mvhd/tkhd duration = %d/%d, want 298144", init.Moov.Mvhd.Duration, init.Moov.Trak.Tkhd.Duration)
	}
	if got := init.Moov.Trak.Mdia.Mdhd.Duration; got != 298_144*44100/1000 {
		t.Fatalf("mdhd duration = %d, want it in the 44.1 kHz media timescale", got)
	}
}

// WarmCBCSKey opens the track's key ahead of playback; the shared prefetch key
// is warmed by the backend itself and must not be sent again per track.
func TestWarmCBCSKeyOpensOnlyTrackKeys(t *testing.T) {
	fp := newFakeFairPlay(t, map[string][]byte{testContentKey: contentAK})
	dialer := DialerFromAddr(fp.ln.Addr().String())

	WarmCBCSKey(context.Background(), dialer, testAdamID, cbcsPrefetchKey)
	WarmCBCSKey(context.Background(), dialer, testAdamID, testContentKey)
	WarmCBCSKey(context.Background(), nil, testAdamID, testContentKey)

	deadline := time.Now().Add(2 * time.Second)
	for len(fp.opened()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := fp.opened(); len(got) != 1 || got[0] != testAdamID+" "+testContentKey {
		t.Fatalf("warmed keys = %q, want only the track key", got)
	}
}
