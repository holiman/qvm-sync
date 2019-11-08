
### Problems

There are a couple of things we want to do as a 'preloader' before unpacking data from another qube:

- Go into `QubesSync/<vmname>/` directory, and remount `.` to disallow `suid` binaries,  
- `chroot' (root-jail) ourselves into the `QubesSync/<vmname>/` directory,
  - `chroot` requires `root` privileges,
- After `chroot`, drop to user `user`.

The forking that 
[qfile-unpacker does](https://github.com/QubesOS/qubes-core-agent-linux/blob/master/qubes-rpc/qfile-unpacker.c) 
is neat -- it forks in the middle of execution, and the forked thread maintains 
`stdin`/`stdout`, and is root-jailed to the `.` directory -- while still 
executing on a binary which resies in `/usr/lib/..`.

A normal `chroot()` is unable to execute files outside of the jail, but from `C`, 
the program is already in memory, and the `fork` elegantly overcomes this limitation.

For Go-lang, it's a bit more problematic, see this [ticket](https://github.com/golang/go/issues/1435) and [example](https://github.com/golang/go/issues/1435#issuecomment-479057768) solution. 
This "kind of" works, but the set up becomes a bit different. 

- Instead of doing in-process fork, we kick of a totally separate process, 
- which is root-jailed as we please, 
- and we can do set-uid to e.g. `nobody`, 
- _but_, we cannot execute binaries that doesn't reside in the root-jail. 


Therefore, we become stuck with a file-layout like this: 

```
home/
  - QubesSync/
    - qvm-receive
    - vm1/
    - vm2/

```
There are some snags with that, in case our `qvm-receive` is compromised by malicious input (or malfunctions), it could
  - modify `vm2` contents, while syncing from `vm1`.
  - become permanently modified, overwriting itself,
 
Not to mention, `qvm-receive` becomes stuck in userland, and cannot be reliably updated as part of distro updates.

So can we use some `C`-based 'preloader' to achieve the same trick as `qfile-unpacker`? That might be possible, 
but it's murky waters for me -- I'm not familiar enough with the go runtime to be certain that it would work
as expected, nor if the solution would be stable over time, or just some temporary hack. 

Another way to get around the problem is by doing the following:

- When `qubes.Filesync` is invoked, 
  1. Copy `/usr/lib/qsync-receive` into `/home/QubesSync/<vmname>/<temp_name>`, with `755` permissions.
  2. Run `qsync-receive` `chroot`:ed
  3. Delete `<temp_name>` again, after execution ends. 

For this to work, it requires `qsync-receive` to be statically linked, and not
have any dynamically loaded dependencies outside of the jail (easily checked 
with `ldd`). Luckily, go-lang shines in this perspective, always (almost) 
producing statically linked binaries[1]. 

[1]: _almost_ -- with the exception of, for example, this particular program, 
which relies on native `C`-libraries to manipulate low-level stuff: 
```
ldd qsync-preloader 
	linux-vdso.so.1 (0x00007fff0c94c000)
	libpthread.so.0 => /lib64/libpthread.so.0 (0x000070e926ab3000)
	libc.so.6 => /lib64/libc.so.6 (0x000070e9268ed000)
	/lib64/ld-linux-x86-64.so.2 (0x000070e926ae9000)

```
Thus, it's important that we avoid littering `qsync-receive` with low level 
bindings. 

The `qsync-preloader` executable implements the scheme above. 
```
echo "test" |  sudo ./qsync-preloader /home/user/go/src/github.com/holiman/qvm-sync/cmd/qsync-receive/qsync-receive 
 [+] Preloader started. Source binary: /home/user/go/src/github.com/holiman/qvm-sync/cmd/qsync-receive/qsync-receive
 [+] Root ok
 [+] Jail dir ok
 [+] Copy to /home/user/QubesSync/all/qsync-receive-temp-5577006791947779410 ok
 [+] Permissions fixed
 [+] Remount ok. Executing call
Error during unpack: could only read 5 bytes, need 32 , err: <nil>
 [+] Call done, cleaned up /home/user/QubesSync/all/qsync-receive-temp-5577006791947779410 ok
Error: exit error: exit status 1

```

