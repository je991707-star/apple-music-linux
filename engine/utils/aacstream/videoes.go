package aacstream

// WebCodecs MV video path (spike): demux a single-track H.264 fMP4 into an
// elementary-stream of access units for the renderer's VideoDecoder — bypassing
// Chromium's MSE ChunkDemuxer (the source of CHUNK_DEMUXER_ERROR_APPEND_FAILED /
// "code=3"). We do the demux here with mp4ff and feed Chromium's *decoder*
// directly via WebCodecs, so the browser demuxer is never involved.
//
// Wire format (big-endian), consumed by the renderer's ES parser:
//
//   Header:
//     magic   [4]byte  = "AME1"
//     codecLen uint16, codec  bytes   (e.g. "avc1.640028")
//     cfgLen   uint16, avcC   bytes   (AVCDecoderConfigurationRecord — VideoDecoder `description`)
//   Then, repeated until EOF, one record per access unit:
//     flags    uint8            (bit0: keyframe)
//     ptsUs    int64            (presentation time, microseconds)
//     durUs    uint32           (duration, microseconds)
//     dataLen  uint32
//     data     [dataLen]byte    (length-prefixed AVCC NAL units)

// var dbgSampleN int // limits [video-es-raw] sample logging (diagnostic)
