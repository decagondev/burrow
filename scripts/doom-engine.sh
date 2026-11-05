# doom-engine.sh - what doomgeneric's engine is, sourced by scripts/doom.sh
# (the fbdev Doom, W11) and scripts/x11-doom.sh (the X window Doom, W16-S05)
# so both build the same pinned commit from the same files. Sets variables
# only; never downloads or builds. Docs: docs/04-plan/waves/WAVE-11-doom.md.
DG_COMMIT=e257a09527062bfb986b6e5e0780839b6cbd5b71
DG_SHA256=26f8f60daa6c6e4d3c01e5eac9fb700d592dede803d13ac848154ff356b2a2e7

# doomgeneric's engine: its Makefile's SRC_DOOM without the X11 backend.
ENGINE="dummy am_map doomdef doomstat dstrings d_event d_items d_iwad d_loop d_main d_mode d_net
 f_finale f_wipe g_game hu_lib hu_stuff info i_cdmus i_endoom i_joystick i_scale i_system
 i_timer memio m_argv m_bbox m_cheat m_controls m_fixed m_menu m_misc m_random p_ceilng
 p_doors p_enemy p_floor p_inter p_lights p_map p_maputl p_mobj p_plats p_pspr p_saveg p_setup
 p_sight p_spec p_switch p_telept p_tick p_user r_bsp r_data r_draw r_main r_plane r_segs r_sky
 r_things sha1 sounds statdump st_lib st_stuff s_sound tables v_video wi_stuff w_checksum w_file
 w_main w_wad z_zone w_file_stdc i_input i_video doomgeneric"

# W22-S03: sound. i_sound.c and m_config.c are built apart, with
# FEATURE_SOUND (i_sound's module tables point at Burrow's module, and it
# binds use_libsamplerate, which m_config's table must then list or Doom
# stops in I_Error), with an empty SDL_mixer.h in place of SDL's, which
# i_sound includes and does not use; user/doom/i_burrowsound.c is
# the module over /dev/dsp. Both Doom builds use the same pair.
SOUND_FLAGS="-DFEATURE_SOUND"
SOUND_STUB="user/doom/sdlstub"
SOUND_MODULE="user/doom/i_burrowsound.c"
SOUND_ENGINE="i_sound m_config"
