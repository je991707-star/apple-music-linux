// Where an fMP4 stream starts on its own timeline, read from its bytes. The engine sends a music video's audio
// with Apple's native timestamps (about 10 s in), so the page has to shift it to line up with the video, and it
// has to know the start BEFORE appending anything. The stream arrives in arbitrary network chunks (often the
// init segment alone), so this answers null until the init segment and the start of the first fragment are both
// complete, and never answers from a half-received box.

const u32 = (b, o) => ((b[o] << 24) | (b[o + 1] << 16) | (b[o + 2] << 8) | b[o + 3]) >>> 0;

// The complete boxes in b[start..end); stops at the first one that is not all there yet.
function boxes(b, start, end) {
    const out = [];
    for (let o = start; o + 8 <= end;) {
        const size = u32(b, o);
        if (size < 8 || o + size > end) break;
        out.push({ type: String.fromCharCode(b[o + 4], b[o + 5], b[o + 6], b[o + 7]), body: o + 8, end: o + size });
        o += size;
    }
    return out;
}
const child = (b, parent, type) => parent && boxes(b, parent.body, parent.end).find(x => x.type === type);

/** First frame's time in seconds (first fragment's decode time / the track's timescale), or null if not known yet. */
export function mp4FirstDecodeSeconds(b) {
    const top = boxes(b, 0, b.length);
    const moov = top.find(x => x.type === 'moov');
    const moof = top.find(x => x.type === 'moof');
    if (!moov || !moof) return null;
    const mdhd = child(b, child(b, child(b, moov, 'trak'), 'mdia'), 'mdhd');
    const tfdt = child(b, child(b, moof, 'traf'), 'tfdt');
    if (!mdhd || !tfdt) return null;
    // Bodies start with version and flags (4 bytes); v1 widens the two times before the timescale to 8 bytes each.
    const timescale = u32(b, mdhd.body + (b[mdhd.body] === 1 ? 20 : 12));
    const decodeTime = b[tfdt.body] === 1
        ? u32(b, tfdt.body + 4) * 2 ** 32 + u32(b, tfdt.body + 8)
        : u32(b, tfdt.body + 4);
    return timescale > 0 ? decodeTime / timescale : null;
}
