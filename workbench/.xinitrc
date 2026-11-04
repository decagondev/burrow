# .xinitrc - your X session (/home/.xinitrc), run by startx. It lasts as
# long as this script: dwm runs until Alt+Shift+q, then the server stops and
# the console comes back. Start programs before dwm with a trailing &, e.g.
#   /opt/x11/bin/st -e /bin/busybox sh &
# Terminals opened from dwm (Alt+Shift+Enter) start in /home.
cd /home
exec /opt/x11/bin/dwm
