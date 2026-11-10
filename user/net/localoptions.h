/* localoptions.h - dropbear's build options for Burrow (W24-S02), copied
 * into dropbear's tree by scripts/net-ports.sh; anything not set here keeps
 * dropbear's default (default_options.h). Its programs live on the NETDATA
 * disk at /opt/net/bin, which a session's PATH must reach (scp runs there),
 * and there is no SFTP server. Docs: docs/04-plan/waves/WAVE-24-ssh-browser.md. */
#define DROPBEAR_PATH_SSH_PROGRAM "/opt/net/bin/dbclient"
#define DEFAULT_PATH "/bin:/opt/net/bin"
#define DEFAULT_ROOT_PATH "/bin:/opt/net/bin"
#define DROPBEAR_SFTPSERVER 0
#define DROPBEAR_X11FWD 0
#define DO_MOTD 0
