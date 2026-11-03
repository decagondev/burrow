Welcome to your Burrow home (/home).

This disk is yours: what you save here survives a power-off. It lives in
burrow-home.img in the repository root (make never overwrites it; delete the
file to start again from these starter files).

  run /bin/busybox sh     a shell (from the burrow> console)
  cd /home
  vi hello.c              edit (i to insert, Esc, :wq to save and quit)
  tcc -static -o hello hello.c
  ./hello 30              run what you compiled
  lua hello.lua           run a Lua script
  exit                    back to the console; poweroff ends the session
