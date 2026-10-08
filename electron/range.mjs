// Parse a single HTTP byte range against a file of `size` bytes.
// Returns { kind: 'full' }                      → serve 200 (no/ignored/multi/malformed Range)
//         { kind: 'range', start, end }         → serve 206, inclusive bounds
//         { kind: 'unsatisfiable' }             → serve 416
export function parseByteRange(rangeHeader, size) {
    const m = rangeHeader && !rangeHeader.includes(',') && /^\s*bytes\s*=\s*(\d*)\s*-\s*(\d*)\s*$/i.exec(rangeHeader);
    if (!m || (m[1] === '' && m[2] === '')) return { kind: 'full' };
    if (size <= 0) return { kind: 'unsatisfiable' };
    let start, end;
    if (m[1] === '') {
        const suffix = Math.min(Number(m[2]), size);
        start = size - suffix; end = size - 1;
        if (suffix <= 0) start = -1;
    } else {
        start = Number(m[1]);
        end = m[2] === '' ? size - 1 : Math.min(Number(m[2]), size - 1);
    }
    if (start < 0 || start >= size || start > end) return { kind: 'unsatisfiable' };
    return { kind: 'range', start, end };
}
