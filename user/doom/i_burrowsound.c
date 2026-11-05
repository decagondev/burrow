/* i_burrowsound.c - Doom's sound effects on Burrow (W22-S03): doomgeneric's
 * sound_module_t over /dev/dsp (OSS, 16-bit stereo at 22050 Hz). Each of
 * eight channels plays one effect, Doom's DMX lump (8-bit unsigned, its own
 * rate, the first and last 16 bytes skipped as DMX does) stepped to the
 * output rate, with its volume and stereo separation; a thread mixes them
 * and writes. It keeps at most QUEUE_BUFFERS of the card's buffers queued,
 * so an effect is heard about 0.1 s after it starts, not after the card's
 * whole ring. No music: DG_music_module does nothing (Doom's MIDI needs an
 * OPL emulator, later). Without /dev/dsp Init fails and Doom plays silent.
 * Never blocks the game's thread on the card. Built with FEATURE_SOUND by
 * scripts/doom.sh and scripts/x11-doom.sh. Docs:
 * docs/04-plan/waves/WAVE-22-sound-tablet.md. */
#include <fcntl.h>
#include <linux/soundcard.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <time.h>
#include <unistd.h>

#include "doomtype.h"
#include "i_sound.h"
#include "m_misc.h"
#include "w_wad.h"
#include "z_zone.h"

#define CHANNELS       8
#define OUT_RATE       22050
#define MIX_FRAMES     512        /* 23 ms a block */
#define QUEUE_BUFFERS  2          /* the card's buffers allowed ahead of the ear */
#define DMX_HEADER     8
#define DMX_PAD        16
#define FIX            16         /* step and position in 16.16 fixed point */

/* i_sound.c binds these when FEATURE_SOUND is on; there is no libsamplerate. */
int use_libsamplerate = 0;
float libsamplerate_scale = 0.65f;

struct sample { const uint8_t *data; uint32_t len, rate; };

struct channel {
    const struct sample *s;   /* NULL: idle */
    uint64_t pos;             /* 16.16, in samples */
    uint32_t step;            /* 16.16 */
    int left, right;          /* 0..255 */
};

static struct channel chans[CHANNELS];
static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_t mixer;
static volatile int running;
static int dsp = -1;
static boolean prefix;

static void lump_name(sfxinfo_t *sfx, char *buf, size_t n) {
    if (sfx->link) sfx = sfx->link;
    if (prefix) M_snprintf(buf, n, "ds%s", sfx->name);
    else M_StringCopy(buf, sfx->name, n);
}

static int get_lump(sfxinfo_t *sfx) {
    char name[9];
    lump_name(sfx, name, sizeof name);
    return W_CheckNumForName(name);
}

/* The lump as a sample, decoded once and kept in driver_data. */
static const struct sample *sample_of(sfxinfo_t *sfx) {
    if (sfx->link) sfx = sfx->link;
    if (sfx->driver_data) return sfx->driver_data;
    if (sfx->lumpnum < 0) sfx->lumpnum = get_lump(sfx);
    if (sfx->lumpnum < 0) return NULL;
    const uint8_t *d = W_CacheLumpNum(sfx->lumpnum, PU_STATIC);
    uint32_t lumplen = W_LumpLength(sfx->lumpnum);
    if (lumplen < DMX_HEADER || d[0] != 0x03 || d[1] != 0x00) return NULL;
    uint32_t rate = d[2] | d[3] << 8, len = d[4] | d[5] << 8 | d[6] << 16 | (uint32_t)d[7] << 24;
    if (len > lumplen - DMX_HEADER || len <= 2 * DMX_PAD + 16 || rate == 0) return NULL;
    struct sample *s = Z_Malloc(sizeof *s, PU_STATIC, 0);
    s->data = d + DMX_HEADER + DMX_PAD;
    s->len = len - 2 * DMX_PAD;
    s->rate = rate;
    sfx->driver_data = s;
    return s;
}

/* Doom's volume 0-127 and separation 0 (left) .. 254 (right). */
static void set_params(struct channel *c, int vol, int sep) {
    int l = (254 - sep) * vol / 127, r = sep * vol / 127;
    c->left = l > 255 ? 255 : l;
    c->right = r > 255 ? 255 : r;
}

static void mix(int16_t *out, int frames) {
    pthread_mutex_lock(&lock);
    for (int i = 0; i < frames; i++) {
        int l = 0, r = 0;
        for (int k = 0; k < CHANNELS; k++) {
            struct channel *c = &chans[k];
            if (!c->s) continue;
            uint32_t at = (uint32_t)(c->pos >> FIX);
            if (at >= c->s->len) { c->s = NULL; continue; }
            int v = (int)c->s->data[at] - 128;     /* -128..127 */
            l += v * c->left;
            r += v * c->right;
            c->pos += c->step;
        }
        l = l > 32767 ? 32767 : l < -32768 ? -32768 : l;
        r = r > 32767 ? 32767 : r < -32768 ? -32768 : r;
        out[2 * i] = (int16_t)l;
        out[2 * i + 1] = (int16_t)r;
    }
    pthread_mutex_unlock(&lock);
}

/* Waits while more than QUEUE_BUFFERS of the card's buffers are queued. */
static void pace(void) {
    struct timespec ms = {0, 2000000};
    for (;;) {
        audio_buf_info info;
        if (ioctl(dsp, SNDCTL_DSP_GETOSPACE, &info) < 0) return;
        if (info.fragstotal - info.fragments <= QUEUE_BUFFERS) return;
        nanosleep(&ms, 0);
    }
}

static void *mixer_main(void *arg) {
    (void)arg;
    static int16_t block[2 * MIX_FRAMES];
    while (running) {
        pace();
        mix(block, MIX_FRAMES);
        if (write(dsp, block, sizeof block) != (ssize_t)sizeof block) break;
    }
    return 0;
}

static boolean snd_init(boolean use_prefix) {
    prefix = use_prefix;
    dsp = open("/dev/dsp", O_WRONLY);
    if (dsp < 0) { printf("i_burrowsound: no /dev/dsp, no sound\n"); return false; }
    int fmt = AFMT_S16_LE, ch = 2, rate = OUT_RATE;
    if (ioctl(dsp, SNDCTL_DSP_SETFMT, &fmt) < 0 || fmt != AFMT_S16_LE ||
        ioctl(dsp, SNDCTL_DSP_CHANNELS, &ch) < 0 || ch != 2 ||
        ioctl(dsp, SNDCTL_DSP_SPEED, &rate) < 0) {
        printf("i_burrowsound: /dev/dsp refused 16-bit stereo, no sound\n");
        close(dsp);
        dsp = -1;
        return false;
    }
    running = 1;
    if (pthread_create(&mixer, 0, mixer_main, 0) != 0) {
        close(dsp);
        dsp = -1;
        return false;
    }
    printf("i_burrowsound: /dev/dsp at %d Hz, %d channels mixed\n", rate, CHANNELS);
    return true;
}

static void snd_shutdown(void) {
    if (dsp < 0) return;
    running = 0;
    pthread_join(mixer, 0);
    close(dsp);
    dsp = -1;
}

static void snd_update(void) {}

static void snd_update_params(int ch, int vol, int sep) {
    if (ch < 0 || ch >= CHANNELS) return;
    pthread_mutex_lock(&lock);
    set_params(&chans[ch], vol, sep);
    pthread_mutex_unlock(&lock);
}

static int snd_start(sfxinfo_t *sfx, int ch, int vol, int sep) {
    if (ch < 0 || ch >= CHANNELS) return -1;
    const struct sample *s = sample_of(sfx);
    if (!s) return -1;
    pthread_mutex_lock(&lock);
    struct channel *c = &chans[ch];
    c->s = s;
    c->pos = 0;
    c->step = (uint32_t)(((uint64_t)s->rate << FIX) / OUT_RATE);
    set_params(c, vol, sep);
    pthread_mutex_unlock(&lock);
    return ch;
}

static void snd_stop(int ch) {
    if (ch < 0 || ch >= CHANNELS) return;
    pthread_mutex_lock(&lock);
    chans[ch].s = NULL;
    pthread_mutex_unlock(&lock);
}

static boolean snd_playing(int ch) {
    if (ch < 0 || ch >= CHANNELS) return false;
    pthread_mutex_lock(&lock);
    boolean on = chans[ch].s != NULL;
    pthread_mutex_unlock(&lock);
    return on;
}

static void snd_cache(sfxinfo_t *sounds, int n) {
    for (int i = 0; i < n; i++) sounds[i].lumpnum = get_lump(&sounds[i]);
}

static snddevice_t devices[] = { SNDDEVICE_SB, SNDDEVICE_PAS, SNDDEVICE_GUS, SNDDEVICE_WAVEBLASTER,
                                 SNDDEVICE_SOUNDCANVAS, SNDDEVICE_AWE32 };

sound_module_t DG_sound_module = {
    devices, sizeof devices / sizeof devices[0],
    snd_init, snd_shutdown, get_lump, snd_update, snd_update_params,
    snd_start, snd_stop, snd_playing, snd_cache,
};

/* No music yet: every call does nothing, and no song ever plays. */
static boolean mus_init(void) { return false; }
static void mus_none(void) {}
static void mus_volume(int v) { (void)v; }
static void *mus_register(void *d, int n) { (void)d; (void)n; return 0; }
static void mus_unregister(void *h) { (void)h; }
static void mus_play(void *h, boolean loop) { (void)h; (void)loop; }
static boolean mus_playing(void) { return false; }

music_module_t DG_music_module = {
    devices, 0, mus_init, mus_none, mus_volume, mus_none, mus_none,
    mus_register, mus_unregister, mus_play, mus_none, mus_playing, NULL,
};
