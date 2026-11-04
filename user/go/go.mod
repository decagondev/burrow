module burrow/user/go

go 1.26.0

// x509roots/fallback (W17-S06, gofetch) needs go 1.26 here; the programs
// keep the GODEBUG defaults they were tested with on Burrow since W12.
godebug default=go1.22

require golang.org/x/crypto/x509roots/fallback v0.0.0-20261103070834-3d426ad4de97
