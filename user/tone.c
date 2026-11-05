/* tone.c - plays two tones through /dev/dsp (W22-S02): an A (440 Hz) for a
 * second at 48000 Hz, 16-bit stereo, then an A an octave up (880 Hz) for half
 * a second at 22050 Hz, unsigned 8-bit mono, so both of the card's paths
 * (as is, and converted) are heard. Built against Linux's
 * <linux/soundcard.h>: if Burrow's request numbers or audio_buf_info differ,
 * this fails. make sound-smoke records the card in a WAV file and
 * tests/smoke/checkwav.py measures both tones. No libm: the sine comes from
 * a rotation. Docs: docs/04-plan/waves/WAVE-22-sound-tablet.md. */
#include <fcntl.h>
#include <linux/soundcard.h>
#include <stdint.h>
#include <stdio.h>
#include <sys/ioctl.h>
#include <unistd.h>

#define PI 3.14159265358979323846

static int set(int fd, unsigned long req, int want, const char *what) {
    int v = want;
    if (ioctl(fd, req, &v) < 0) { printf("tone: FAIL %s\n", what); return -1; }
    return v;
}

/* sin and cos of a small angle by their series: enough for one step. */
static void step(double w, double *c, double *s) {
    double w2 = w * w;
    *c = 1 - w2 / 2 + w2 * w2 / 24 - w2 * w2 * w2 / 720;
    *s = w - w * w2 / 6 + w * w2 * w2 / 120 - w * w2 * w2 * w2 / 5040;
}

/* Plays hz for ms at rate, 16-bit stereo or 8-bit mono. */
static int play(int fd, int hz, int ms, int rate, int s16) {
    double c, s, x = 1, y = 0;   /* the point (x, y) turns by w each sample */
    step(2 * PI * hz / rate, &c, &s);
    uint8_t buf[4096];
    int n = 0, frames = rate * ms / 1000;
    for (int i = 0; i < frames; i++) {
        double nx = x * c - y * s;
        y = x * s + y * c;
        x = nx;
        if (s16) {
            int16_t v = (int16_t)(y * 12000);
            buf[n++] = (uint8_t)v; buf[n++] = (uint8_t)(v >> 8);
            buf[n++] = (uint8_t)v; buf[n++] = (uint8_t)(v >> 8);
        } else {
            buf[n++] = (uint8_t)(128 + (int)(y * 90));
        }
        if (n == sizeof buf || i == frames - 1) {
            if (write(fd, buf, n) != n) { puts("tone: FAIL write"); return -1; }
            n = 0;
        }
    }
    return 0;
}

int main(void) {
    int fd = open("/dev/dsp", O_WRONLY);
    if (fd < 0) { puts("tone: FAIL no /dev/dsp"); return 1; }
    if (set(fd, SNDCTL_DSP_SETFMT, AFMT_S16_LE, "SETFMT") != AFMT_S16_LE ||
        set(fd, SNDCTL_DSP_CHANNELS, 2, "CHANNELS") != 2 ||
        set(fd, SNDCTL_DSP_SPEED, 48000, "SPEED") != 48000) {
        puts("tone: FAIL set-up");
        return 1;
    }
    audio_buf_info info;
    if (ioctl(fd, SNDCTL_DSP_GETOSPACE, &info) < 0) { puts("tone: FAIL GETOSPACE"); return 1; }
    printf("tone: 48000 Hz s16 stereo, %d of %d fragments of %d bytes free\n",
           info.fragments, info.fragstotal, info.fragsize);
    if (play(fd, 440, 1000, 48000, 1)) return 1;
    if (ioctl(fd, SNDCTL_DSP_SYNC, 0) < 0) { puts("tone: FAIL SYNC"); return 1; }
    if (set(fd, SNDCTL_DSP_SPEED, 22050, "SPEED") != 22050 ||
        set(fd, SNDCTL_DSP_SETFMT, AFMT_U8, "SETFMT") != AFMT_U8 ||
        set(fd, SNDCTL_DSP_CHANNELS, 1, "CHANNELS") != 1) {
        puts("tone: FAIL second set-up");
        return 1;
    }
    puts("tone: 22050 Hz u8 mono");
    if (play(fd, 880, 500, 22050, 0)) return 1;
    int delay = -1;
    ioctl(fd, SNDCTL_DSP_GETODELAY, &delay);
    printf("tone: %d bytes still to play\n", delay);
    close(fd);   /* drains */
    puts("tone: ok");
    return 0;
}
