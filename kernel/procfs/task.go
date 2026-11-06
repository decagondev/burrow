package procfs

// task.go - /proc/<pid>/task/ (W23-S02): a directory per thread with its
// own stat, status and comm, which carry that thread's id, state and CPU
// time and the process's everything else, as Linux's do. Docs:
// WAVE-23-proc-virtio.md.

import (
	"io/fs"
	"strconv"

	"burrow/kernel/fsattr"
)

// taskFiles are the files in every task/<tid>/.
var taskFiles = []string{"comm", "stat", "status"}

const inoTask = 1 << 62 // task inodes: pid, tid and file above this

// taskNode is task/, task/<tid>/ or one of its files, for p.
func (f *FS) taskNode(p *Proc, a fsattr.Attr, rest []string) (node, error) {
	if len(rest) == 0 {
		a.Ino += uint64(len(pidFiles) + 1)
		return f.dir("task", a, func() []string {
			names := make([]string, len(p.Tasks))
			for i, t := range p.Tasks {
				names[i] = strconv.Itoa(t.TID)
			}
			return names
		}), nil
	}
	tid, err := strconv.Atoi(rest[0])
	var task *Task
	for i := range p.Tasks {
		if err == nil && p.Tasks[i].TID == tid && rest[0] == strconv.Itoa(tid) {
			task = &p.Tasks[i]
		}
	}
	if task == nil || len(rest) > 2 {
		return node{}, fs.ErrNotExist
	}
	tp := *p // the thread's view: its id, state and time
	tp.TGID, tp.PID, tp.State, tp.User, tp.Sys = p.PID, task.TID, task.State, task.User, task.Sys
	a.Ino = inoTask | uint64(p.PID)<<24 | uint64(tid)<<4
	if len(rest) == 1 {
		return f.dir(rest[0], a, func() []string { return taskFiles }), nil
	}
	for i, name := range taskFiles {
		if name != rest[1] {
			continue
		}
		for _, pf := range pidFiles {
			if pf.name == name {
				a.Ino += uint64(i + 1)
				return f.file(name, pf.mode, a, func() []byte { return pf.gen(f, &tp) }), nil
			}
		}
	}
	return node{}, fs.ErrNotExist
}
