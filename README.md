## Qvm-sync

`qvm-sync` is inspired by `qvm-copy`, and [this ticket](). It extends the 
functionality of `qvm-copy`, by only copying missing or modified files
from one `qube` to another


## Why go-lang

I wanted to write it in go-lang because I'm more proficient in go-lang than C, 
and felt more comfortable writing security-critical code using it. 

It turned out that some tricks were not available from within go-lang 
(see [the preloader readme](./cmd/qsync-preloader/README.md)), but I _think_
the obstacles were overcome in the end. 

Another snag was that it doesn't seem possble to 
- set permissions on symlinks, nor
- set mtime/atime on symlinks [ticket](https://github.com/golang/go/issues/3951)

Building it in go-lang made it easy to implement proper testcase support, and
if someone wants to port it over to C, they can use my implementation as a base. 


## Installation

On at least one VM, you need to have `go` installed. On that vm, simply run
`install.sh`, and you can later `qvm-copy` the binaries across vms. 
The `install.sh` scripts requires the binaries and the scripts in the `./scripts` 
folder. 

## How it works

### How `qvm-copy` works

In order to build this, I had to dive deep into how qubes-rpc and `qvm-filecopy`
works under the hood. 

When the command `qvm-copy foo` in invoked, it is bash script, which (eventually)
resolved to

```
qrexec-client-vm @default qubes.Filecopy /usr/lib/qubes/qfile-agent foo
```

The `qrexec-client-vm` command does this:

- Asks to invoke the rpc-service `qubes.Filecopy` on the remote side, 
- prompting `qubes os` to pop up a dialog for confirmation, and
- starts the local binary `/usr/lib/qubes/qfile-agent` with the argument `foo`

The `qfile-agent` spits out the files on `stdout`.

```

$ echo "test">  test.txt
$ /usr/lib/qubes/qfile-agent test.txt | xxd
00000000: 0900 0000 b481 0000 0500 0000 0000 0000  ................
00000010: 2e28 c05d c008 9e2a 2928 c05d 8078 2514  .(.]...*)(.].x%.
00000020: 7465 7374 2e74 7874 0074 6573 740a 0000  test.txt.test...
00000030: 0000 0000 0000 0000 0000 0000 0000 0000  ................

```
If `foo` is a directory, it walks through it and spits out each file. Directories
and symlinks are handled a bit differently. The transmission contains both the 
metadata (permissions, mtime etc) and the actual data content. 

On the receiving end, there's the service defined in 
`/etc/qubes-rpc/qubes.Filecopy`, which looks like this:

```bash
#!/usr/bin/sh
exec /usr/lib/qubes/qfile-unpacker

```
It contains a [shim](https://github.com/QubesOS/qubes-core-agent-linux/blob/master/qubes-rpc/qfile-unpacker.c)
which, when executed as `root`, root-jails the actual unpacker and drops back
to `user`. 

The unpacker/parser does the actual unpacking on the receiver side. Due to the
root-jail, it is incapable of writing outside of `/home/user/QubesIncoming/<source-vm>/`.


A rough scheme of how it works is
```
A -> invoke listener on B
A -> sends [files] to B. Listens for confirmation 
B -> sends confirmation to A, 
A -> Hangs up
```

The actual sources for this can be found here: 

* [qrexec-lib](https://github.com/QubesOS/qubes-linux-utils/tree/master/qrexec-lib)
* [unpacker shim](https://github.com/QubesOS/qubes-core-agent-linux/blob/master/qubes-rpc/qfile-unpacker.c)


### How `qvm-sync` works

The `qvm-sync` scheme is roughly similar, but, 

- Instead of sending the file contents, with the first send, we only send the metadata. 
- As metadata is received, the receiver checks if it already has the file.
  - If it does not have file, add to `requestlist`. 
  - If it has the file, but metadata differs, add to `requestlist'
- Send back requestlist, which is only a list of indexes.
- The initiator then:
  - For each file in requestlist, send over to receiver. 
 
The first version, currently implemented, is pretty dumb. It does not use 
`crc` on files, but only checks size/mtime/atime for differences. 

It probably makes sense to use crc sums on files, but I'll have to evaluate it 
in practice a bit first to see how well/bad this dump approach works, before I 
add that. 

The indices do not count directories, so for syncing the following directory: 
```
a/
 - foo
 - b/
   - bar
```
The following data is sent, (with indices in parenthesis -- not actually transmitted over the wire):
```
a       (none)
a/foo   (0)
a/b/    (none)
a/b/bar (1)
a/b/    (none) // end-dir marker
a/      (none) // end-dir marker
EOT     (none) // end-of-transfer marker
```

### Notes

#### About the protocol

The protocol is intentionally kept as close to `qvm-copy` as possible, and has 
avoided any parts that would require multithreading. The entire thing is based on
four sequential steps, and thus it should be fairly simple to extend `qvm-copy` to 
implement `qvm-sync`, if one wanted to. 

#### Known issues

The go-lang implementation has a few known bugs: 

- It is unable to set `permissions` on symlinks, 
- It is unable to sync `mtime/atime` on symlinks,

#### Security

The `qsync-preloader` is meant to be run as a `suid` binary -- i.e. privileged. It
is does not contain any imports other than the golang base libraries, so no 
external dependencies of any kind (that goes for all three parts). 

In general, go-lang is memory-safe, and typically crashes rather than continues
in a bad (insecure) state if corruption occurs. 

The implementation does not make use of golang unsafe pointers.