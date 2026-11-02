; lib16.inc - tiny 16-bit real-mode console helpers shared by stage1 and stage2.
; Interface (keep it this small - Interface Segregation):
;   serial_init        : COM1 115200 8N1
;   putc  (AL)         : one char to COM1 + BIOS teletype
;   print (DS:SI)      : NUL-terminated string

; ---------------------------------------------------------------------------
; serial_init - COM1 115200 8N1, FIFO on. Polling only, no IRQs.
; ---------------------------------------------------------------------------
serial_init:
    mov dx, COM1 + 1
    xor al, al
    out dx, al                   ; IER = 0: disable UART interrupts
    mov dx, COM1 + 3
    mov al, 0x80
    out dx, al                   ; LCR: DLAB=1 to set baud divisor
    mov dx, COM1 + 0
    mov al, 1
    out dx, al                   ; divisor low  = 1  -> 115200 baud
    mov dx, COM1 + 1
    xor al, al
    out dx, al                   ; divisor high = 0
    mov dx, COM1 + 3
    mov al, 0x03
    out dx, al                   ; LCR: 8 data bits, no parity, 1 stop, DLAB=0
    mov dx, COM1 + 2
    mov al, 0xC7
    out dx, al                   ; FCR: enable + clear FIFOs, 14-byte threshold
    ret

; ---------------------------------------------------------------------------
; putc - AL = char. Writes to COM1 (polled) AND the BIOS teletype.
; Clobbers: nothing visible to callers except flags.
; ---------------------------------------------------------------------------
putc:
    push ax
    push bx
    push dx
    mov bl, al
    mov dx, COM1 + 5             ; LSR
.wait:
    in  al, dx
    test al, 0x20                ; THR empty?
    jz  .wait
    mov dx, COM1
    mov al, bl
    out dx, al
    mov ah, 0x0E                 ; BIOS teletype
    mov bh, 0
    int 0x10
    pop dx
    pop bx
    pop ax
    ret

; print - DS:SI -> NUL-terminated string
print:
    lodsb
    test al, al
    jz  .done
    call putc
    jmp print
.done:
    ret

