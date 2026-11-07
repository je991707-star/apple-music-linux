package export

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"time"
)

// FFmpegInfo is what the engine can truthfully say about the ffmpeg that exports would run: that it was found
// AND that it runs, with its version and the encoders the app uses. A file called ffmpeg that is missing a DLL,
// is the wrong architecture or hangs is reported as unavailable, with the reason.
type FFmpegInfo struct {
	Available bool            `json:"available"`
	Path      string          `json:"path,omitempty"`
	Version   string          `json:"version,omitempty"`
	Error     string          `json:"error,omitempty"`
	Encoders  map[string]bool `json:"encoders,omitempty"` // aac, flac, libx264: whether this build has them
}

// probedEncoders are the encoders the app can ask ffmpeg for: re-encoding downloads to AAC or FLAC, and
// HEVC music videos to H.264 (which only libx264 can do).
var probedEncoders = []string{"aac", "flac", "libx264"}

const probeTimeout = 4 * time.Second

// ProbeFFmpeg resolves ffmpeg the way exports do (the custom path if one is given and acceptable, else the one on
// PATH) and runs it. custom is the user's "FFmpeg path" setting; it is validated like options.ffmpegPath.
func ProbeFFmpeg(ctx context.Context, custom string) FFmpegInfo {
	if !validToolPath(custom, "ffmpeg") {
		return FFmpegInfo{Error: "the path must name an ffmpeg binary"}
	}
	if err := checkToolExecutable(custom, "ffmpeg"); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return FFmpegInfo{Error: "not found"}
		}
		return FFmpegInfo{Error: err.Error()}
	}
	name := custom
	if name == "" {
		name = "ffmpeg"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return FFmpegInfo{Error: "not found"}
	}
	info := FFmpegInfo{Path: path}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := toolCommand(ctx, path, "-hide_banner", "-version").Output()
	if err != nil {
		info.Error = "found, but it would not run: " + err.Error()
		return info
	}
	if info.Version = ffmpegVersion(string(out)); info.Version == "" {
		info.Error = "found, but it did not identify itself as ffmpeg"
		return info
	}
	info.Available = true
	if enc, err := toolCommand(ctx, path, "-hide_banner", "-encoders").Output(); err == nil {
		info.Encoders = ffmpegEncoders(string(enc))
	}
	return info
}

// ffmpegVersion reads "ffmpeg version 9.0.2 Copyright ..." (also "n7.1-4-g..." style builds).
func ffmpegVersion(out string) string {
	line, _, _ := strings.Cut(out, "\n")
	parts := strings.Fields(line)
	if len(parts) >= 3 && parts[0] == "ffmpeg" && parts[1] == "version" {
		return parts[2]
	}
	return ""
}

// ffmpegEncoders reads `ffmpeg -encoders`: after a "------" line, each row is "<flags> <name> <description>".
func ffmpegEncoders(out string) map[string]bool {
	have := map[string]bool{}
	listing := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "------") {
			listing = true
			continue
		}
		if !listing {
			continue
		}
		if f := strings.Fields(line); len(f) >= 2 {
			have[f[1]] = true
		}
	}
	found := make(map[string]bool, len(probedEncoders))
	for _, e := range probedEncoders {
		found[e] = have[e]
	}
	return found
}
