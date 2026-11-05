; vbe16.inc - stage2's VBE mode set (W21-S01). Included in stage2's real-mode
; part; uses disk_read, find_entry and fail from there.
;
; If the manifest has a "video" payload (struct video_request, manifest.h),
; vbe_setup walks the VBE mode list for a linear direct-colour mode of that
; width, height and depth, sets it, and asks for a padded scanline when the
; request names one. What it found stays for boot_info (S02): the chosen
; ModeInfoBlock at VBE_MODE_ADDR, the mode in vbe_mode, the pitch the card
; accepted in vbe_pitch. Without a payload it does nothing.
;
; Never prints through the BIOS teletype (putc): once the mode is set there
; is no text mode, so its messages go to COM1 only (sputc). Never sets a
; mode it did not find in the BIOS's own list.
; VESA BIOS Extension 2.0: 4F00 controller info, 4F01 mode info, 4F02 set
; mode, 4F06 logical scanline. Docs: docs/04-plan/waves/WAVE-21-framebuffer.md.

VBE_OK           equ 0x004F
VBE_ATTR_NEED    equ 0x0091       ; ModeAttributes: supported (0), graphics (4), linear framebuffer (7)
VBE_MODEL_DIRECT equ 6            ; MemoryModel: direct colour
VBE_LINEAR       equ 0x4000       ; 4F02: use the linear framebuffer
VBE_SIG_VBE2     equ 0x32454256   ; "VBE2": ask for the VBE 2.0 controller info
VBE_SCAN_SET_B   equ 2            ; 4F06 BL: set the scanline in bytes
VBE_INFO_MODES   equ 0x0E         ; VbeInfoBlock: far pointer to the mode list
MI_ATTR          equ 0x00         ; ModeInfoBlock fields
MI_PITCH         equ 0x10
MI_WIDTH         equ 0x12
MI_HEIGHT        equ 0x14
MI_BPP           equ 0x19
MI_MODEL         equ 0x1B
MI_BASE          equ 0x28
MODE_LIST_END    equ 0xFFFF

; ---------------------------------------------------------------------------
; vbe_setup - the mode the video payload asks for, if any.
; Clobbers: EAX, EBX, ECX, EDX, ESI, EDI, FS.
; ---------------------------------------------------------------------------
vbe_setup:
    mov di, name_video
    call find_entry
    jc  .ret                      ; no payload: no VBE (the kernel uses DISPI)
    mov eax, [si + ME_LBA]
    mov cx, 1
    xor dx, dx
    mov bx, VBE_REQ_ADDR
    call disk_read
    cmp dword [VBE_REQ_ADDR + VR_MAGIC], VIDEO_MAGIC_DW
    jne .bad
    mov dword [VBE_INFO_ADDR], VBE_SIG_VBE2
    mov di, VBE_INFO_ADDR
    mov ax, 0x4F00
    int 0x10
    cmp ax, VBE_OK
    jne .no_mode
    lfs si, [VBE_INFO_ADDR + VBE_INFO_MODES]
.next:
    mov cx, [fs:si]
    add si, 2
    cmp cx, MODE_LIST_END
    je  .no_mode
    call vbe_mode_matches
    jne .next
    mov [vbe_mode], cx
    mov bx, cx
    or  bx, VBE_LINEAR
    mov ax, 0x4F02
    int 0x10
    cmp ax, VBE_OK
    jne .bad
    movzx eax, word [VBE_MODE_ADDR + MI_PITCH]
    mov [vbe_pitch], eax
    mov ecx, [VBE_REQ_ADDR + VR_PITCH]
    test ecx, ecx
    jz  .report
    mov bl, VBE_SCAN_SET_B
    mov ax, 0x4F06
    int 0x10                      ; CX = bytes asked; BX = bytes the card took
    cmp ax, VBE_OK
    jne .report                   ; refused: the marker shows the mode's own pitch
    movzx ebx, bx
    mov [vbe_pitch], ebx
.report:                          ; "[stage2] vbe: WxHxB pitch P at BASE (mode 0xM)"
    mov si, msg_vbe
    call sprint
    mov ax, [VBE_MODE_ADDR + MI_WIDTH]
    mov bx, [VBE_MODE_ADDR + MI_HEIGHT]
    mov dl, [VBE_MODE_ADDR + MI_BPP]
    call swhb
    mov si, msg_vbe_pitch
    call sprint
    mov eax, [vbe_pitch]
    call sdec
    mov si, msg_vbe_at
    call sprint
    mov eax, [VBE_MODE_ADDR + MI_BASE]
    mov cx, 8
    call shex
    mov si, msg_vbe_mode
    call sprint
    movzx eax, word [vbe_mode]
    mov cx, 3
    call shex
    mov si, msg_vbe_end
    jmp sprint                    ; tail call
.no_mode:                         ; "[stage2] vbe: no WxHxB mode": boot on, without one
    mov si, msg_vbe_no
    call sprint
    mov ax, [VBE_REQ_ADDR + VR_WIDTH]
    mov bx, [VBE_REQ_ADDR + VR_HEIGHT]
    mov dl, [VBE_REQ_ADDR + VR_BPP]
    call swhb
    mov si, msg_vbe_none
    jmp sprint
.bad:                             ; a corrupt request, or a listed mode that will not set
    mov al, 'V'
    jmp fail
.ret:
    ret

; vbe_mode_matches - CX = a mode number: ZF=1 if it is linear, direct colour
; and the requested size and depth. Its ModeInfoBlock stays at VBE_MODE_ADDR.
; Preserves CX, SI, FS. Clobbers: AX, DI.
vbe_mode_matches:
    push si
    push fs
    push cx
    mov di, VBE_MODE_ADDR
    mov ax, 0x4F01
    int 0x10
    pop cx
    pop fs
    pop si
    cmp ax, VBE_OK
    jne .ret
    mov ax, [VBE_MODE_ADDR + MI_ATTR]
    and ax, VBE_ATTR_NEED
    cmp ax, VBE_ATTR_NEED
    jne .ret
    cmp byte [VBE_MODE_ADDR + MI_MODEL], VBE_MODEL_DIRECT
    jne .ret
    mov ax, [VBE_MODE_ADDR + MI_WIDTH]
    cmp ax, [VBE_REQ_ADDR + VR_WIDTH]
    jne .ret
    mov ax, [VBE_MODE_ADDR + MI_HEIGHT]
    cmp ax, [VBE_REQ_ADDR + VR_HEIGHT]
    jne .ret
    mov al, [VBE_MODE_ADDR + MI_BPP]
    cmp al, [VBE_REQ_ADDR + VR_BPP]
.ret:
    ret

; ---- serial-only output (the screen may be in a graphics mode) -------------
; sputc - AL to COM1. Clobbers: flags.
sputc:
    push dx
    push ax
    mov dx, COM1 + 5              ; LSR
.wait:
    in  al, dx
    test al, 0x20                 ; THR empty?
    jz  .wait
    pop ax
    mov dx, COM1
    out dx, al
    pop dx
    ret

; sprint - DS:SI NUL-terminated string to COM1. Clobbers: SI, AL.
sprint:
    lodsb
    test al, al
    jz  .done
    call sputc
    jmp sprint
.done:
    ret

; sdec - EAX in decimal to COM1. Clobbers: EAX, CX, EDX.
sdec:
    push ebx
    mov ebx, 10
    xor cx, cx
.div:
    xor edx, edx
    div ebx
    push dx
    inc cx
    test eax, eax
    jnz .div
.out:
    pop ax
    add al, '0'
    call sputc
    loop .out
    pop ebx
    ret

; shex - the low CX (1..8) hex digits of EAX to COM1. Clobbers: EAX, CX, DX.
shex:
    mov dx, cx
    mov cl, 8
    sub cl, dl
    shl cl, 2
    shl eax, cl                   ; the first digit wanted is now the top one
    mov cx, dx
.digit:
    rol eax, 4
    push eax
    and al, 0x0F
    add al, '0'
    cmp al, '9'
    jbe .put
    add al, 'a' - '0' - 10
.put:
    call sputc
    pop eax
    loop .digit
    ret

; swhb - "WxHxB" to COM1: AX = width, BX = height, DL = bits per pixel.
; Clobbers: EAX, CX, EDX.
swhb:
    push dx
    movzx eax, ax
    call sdec
    mov al, 'x'
    call sputc
    movzx eax, bx
    call sdec
    mov al, 'x'
    call sputc
    pop dx
    movzx eax, dl
    jmp sdec                      ; tail call
