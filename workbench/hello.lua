-- hello.lua - a starter script in your Burrow home: run it with `lua hello.lua`.
local words = {}
for w in ("edit save build run"):gmatch("%a+") do
  words[#words + 1] = w:upper()
end
print(table.concat(words, " -> ") .. ", all on Burrow")
