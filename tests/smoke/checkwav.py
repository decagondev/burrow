#!/usr/bin/env python3
"""checkwav.py - what a QEMU -audiodev wav file heard (W22-S02).

Usage: checkwav.py <file.wav> tone
       checkwav.py <file.wav> sounds <at-least>

tone: the file must hold user/tone.c's two tones, in order: 440 Hz for at
least 0.9 s, then 880 Hz for at least 0.4 s, each within 2 %, told apart
by the pitch of each 100 ms (they may follow each other with no silence).
sounds: at least that many separate sounds (a game's effects: Doom's menu
and a pistol shot in make doom-smoke).

Sounds are found as runs of 10 ms windows whose level is above a threshold;
a run's pitch is its zero crossings per second over two. QEMU may leave the
RIFF sizes at 0 if it was stopped hard, so the data is read to the end of
the file. Exit 1 with the reason on any failure.
Docs: docs/04-plan/waves/WAVE-22-sound-tablet.md.
"""
import struct
import sys

LEVEL = 1500          # 16-bit amplitude that counts as sound
WINDOW_S = 0.010
MIN_GAP_S = 0.030     # a shorter quiet stretch does not split a sound
TOLERANCE = 0.02
PITCH_WINDOW_S = 0.100 # 44 periods of 440 Hz: one crossing more or less is 1 %


def read_wav(path):
    b = open(path, 'rb').read()
    if b[:4] != b'RIFF' or b[8:12] != b'WAVE':
        sys.exit(f'checkwav: {path} is not a WAV file')
    pos, fmt, data = 12, None, None
    while pos + 8 <= len(b):
        cid, size = b[pos:pos + 4], struct.unpack_from('<I', b, pos + 4)[0]
        body = b[pos + 8:]
        if cid == b'fmt ':
            fmt = struct.unpack_from('<HHIIHH', body)
        if cid == b'data':
            data = body if size == 0 or size > len(body) else body[:size]
            break
        pos += 8 + size + (size & 1)
    if not fmt or data is None:
        sys.exit('checkwav: no fmt or data chunk')
    tag, channels, rate, _, _, bits = fmt
    if tag != 1 or bits != 16:
        sys.exit(f'checkwav: format {tag}, {bits} bits: want 16-bit PCM')
    n = len(data) // (2 * channels)
    left = struct.unpack_from(f'<{n * channels}h', data)[::channels]
    return rate, left


def sounds(rate, s):
    """Runs of loud windows: [(start_s, end_s, samples)]."""
    w = max(1, int(rate * WINDOW_S))
    loud = [max(abs(v) for v in s[i:i + w]) > LEVEL for i in range(0, len(s) - w + 1, w)]
    runs, start, quiet = [], None, 0
    for i, on in enumerate(loud + [False] * (int(MIN_GAP_S / WINDOW_S) + 1)):
        if on:
            start = i if start is None else start
            quiet = 0
        elif start is not None:
            quiet += 1
            if quiet * WINDOW_S >= MIN_GAP_S:
                end = i - quiet + 1
                runs.append((start * WINDOW_S, end * WINDOW_S, s[start * w:end * w]))
                start, quiet = None, 0
    return runs


def pitch(rate, s):
    crossings = sum(1 for a, b in zip(s, s[1:]) if (a < 0) != (b < 0))
    return crossings / 2 / (len(s) / rate)


def main():
    path, mode = sys.argv[1], sys.argv[2]
    rate, left = read_wav(path)
    found = sounds(rate, left)
    desc = ', '.join(f'{a:.2f}-{b:.2f} s at {pitch(rate, x):.0f} Hz' for a, b, x in found)
    print(f'checkwav: {path}: {rate} Hz, {len(left) / rate:.2f} s, sounds: {desc or "none"}')
    if mode == 'tone':
        # The two tones may meet with no silence between them (SYNC, then a
        # new rate at once), so label 100 ms windows by their own pitch and
        # look for a run of 440 Hz, then a run of 880 Hz.
        w = int(rate * PITCH_WINDOW_S)
        labels = []
        for _, _, x in found:
            for i in range(0, len(x) - w + 1, w):
                got = pitch(rate, x[i:i + w])
                labels.append(next((hz for hz in (440, 880) if abs(got - hz) <= hz * TOLERANCE), 0))
        runs = []
        for hz in labels:
            if runs and runs[-1][0] == hz:
                runs[-1][1] += PITCH_WINDOW_S
            else:
                runs.append([hz, PITCH_WINDOW_S])
        tones = [(hz, d) for hz, d in runs if hz and d >= 0.2]
        print('checkwav: tones ' + ', '.join(f'{hz} Hz for {d:.1f} s' for hz, d in tones))
        if [hz for hz, _ in tones] != [440, 880] or tones[0][1] < 0.9 - PITCH_WINDOW_S or tones[1][1] < 0.4 - PITCH_WINDOW_S:
            sys.exit('checkwav: FAIL want 440 Hz for 0.9 s, then 880 Hz for 0.4 s')
        print('checkwav: PASS (440 Hz, then 880 Hz)')
    elif mode == 'sounds':
        least = int(sys.argv[3])
        if len(found) < least:
            sys.exit(f'checkwav: FAIL {len(found)} separate sounds, want at least {least}')
        print(f'checkwav: PASS ({len(found)} separate sounds)')
    else:
        sys.exit(f'checkwav: unknown mode {mode}')


if __name__ == '__main__':
    main()
