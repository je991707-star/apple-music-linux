import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mp4FirstDecodeSeconds } from '../../src/engine/mp4base.js';

const u32 = (n) => [(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255];
const box = (type, ...parts) => {
    const body = parts.flat();
    return [...u32(8 + body.length), ...[...type].map((c) => c.charCodeAt(0)), ...body];
};
// Per ISO 14496-12: mdhd v0 is ver/flags, creation, modification, timescale, duration; v1 uses 8-byte times.
const mdhd = (timescale, v1) => v1
    ? box('mdhd', [1, 0, 0, 0], new Array(16).fill(0), u32(timescale), new Array(8).fill(0), [0, 0, 0, 0])
    : box('mdhd', [0, 0, 0, 0], u32(0x11111111), u32(0x22222222), u32(timescale), u32(0), [0, 0, 0, 0]);
const init = (timescale, v1) => [
    ...box('ftyp', [..."isom"].map((c) => c.charCodeAt(0)), u32(0)),
    ...box('moov', box('trak', box('mdia', mdhd(timescale, v1)))),
];
const tfdt = (time, v1) => v1 ? box('tfdt', [1, 0, 0, 0], u32(Math.floor(time / 2 ** 32)), u32(time >>> 0))
    : box('tfdt', [0, 0, 0, 0], u32(time));
const fragment = (time, v1) => [...box('moof', box('mfhd', [0, 0, 0, 0], u32(1)), box('traf', box('tfhd', [0, 0, 0, 0], u32(1)), tfdt(time, v1))),
    ...box('mdat', new Array(64).fill(7))];

test('reads the first fragment time over the track timescale', () => {
    // Apple's music-video audio: 48 kHz, first fragment at 480000 ticks = 10 s.
    assert.equal(mp4FirstDecodeSeconds(Uint8Array.from([...init(48000), ...fragment(480000)])), 10);
    assert.equal(mp4FirstDecodeSeconds(Uint8Array.from([...init(44100, true), ...fragment(441000, true)])), 10);
    assert.equal(mp4FirstDecodeSeconds(Uint8Array.from([...init(48000), ...fragment(0)])), 0);
    // A 64-bit decode time past 2^32 ticks.
    assert.equal(mp4FirstDecodeSeconds(Uint8Array.from([...init(1000, true), ...fragment(2 ** 33 + 500, true)])), (2 ** 33 + 500) / 1000);
});

test('an init segment on its own is not enough to tell', () => {
    // This is the chunk the network often delivers first, and the one that used to leave the audio 10 s late.
    assert.equal(mp4FirstDecodeSeconds(Uint8Array.from(init(48000))), null);
    assert.equal(mp4FirstDecodeSeconds(new Uint8Array(0)), null);
});

test('whatever way the stream is cut, the answer is null or correct, never wrong', () => {
    for (const v1 of [false, true]) {
        const stream = Uint8Array.from([...init(48000, v1), ...fragment(480000, v1)]);
        let firstAnswer = -1;
        for (let n = 0; n <= stream.length; n++) {
            const got = mp4FirstDecodeSeconds(stream.subarray(0, n));
            assert.ok(got === null || got === 10, `v1=${v1}, ${n} bytes: ${got}`);
            if (got === 10 && firstAnswer < 0) firstAnswer = n;
        }
        assert.ok(firstAnswer > 0, 'the full stream must be answerable');
        // Once the fragment's moof is complete the answer must not wait for the media data after it.
        assert.ok(firstAnswer < stream.length, 'does not need the mdat');
    }
});

test('garbage and unrelated boxes give null', () => {
    assert.equal(mp4FirstDecodeSeconds(Uint8Array.from([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12])), null);
    assert.equal(mp4FirstDecodeSeconds(Uint8Array.from(box('free', [1, 2, 3]))), null);
});
