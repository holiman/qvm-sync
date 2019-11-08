package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	destUser = "user"
	destRoot = "/home/user/QubesSync"
)

var logger *log.Logger

func init() {
	host, _ := os.Hostname()
	name := filepath.Base(os.Args[0])
	prefix := fmt.Sprintf(" [%v@%s] ", name, host)
	log.SetPrefix(prefix)
	log.SetOutput(os.Stderr)
}

func main() {
	if len(os.Args) < 2 {
		log.Print("Error, no executable specified!")
		log.Fatalf("usage:\n %v <path-to-executable>", os.Args[0])
	}
	sourceBinary := os.Args[1]
	log.Printf("Preloader started. Source binary: %v", sourceBinary)
	if err := execJailed(destUser, destRoot, sourceBinary); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
}

// setupDir creates the given directory as 0700, sets the uid/gid ownership,
// and chdirs into it
func setupDir(dir string, uid, gid int) (string, error) {
	// Create directories
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	// Make 'user' own then
	if err := os.Chown(dir, uid, gid); err != nil {
		return "", fmt.Errorf("failed re-owning %v by %v", dir, uid)
	}
	// Change into it
	if err := os.Chdir(dir); err != nil {
		return "", fmt.Errorf("failed chdir: %v", err)
	}
	return dir, nil
}

func copyFile(src, dest string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer to.Close()
	_, err = io.Copy(to, from)
	return err
}

// switchUser comes mostly from
// https://github.com/golang/go/issues/1435#issuecomment-479057768
// by @larytet
func execJailed(uname, jail, trustedBinary string) error {
	var (
		err error
		usr *user.User
	)
	// Are we root? If we are running a suid binary, we need to check the
	// EUID (effective UID), not the UID (original UID)
	if uid := syscall.Geteuid(); uid != 0 {
		return fmt.Errorf("need root credentials, got %v", uid)
	}
	log.Printf("Root ok")
	// Does 'user' exist?
	if usr, err = user.Lookup(uname); err != nil {
		return fmt.Errorf("failed to lookup '%s' %v", uname, err)
	}
	gid, _ := strconv.Atoi(usr.Gid)
	uid, _ := strconv.Atoi(usr.Uid)
	// Is it some weird root alias?
	if uid == syscall.Geteuid() {
		// Let's forbid root aliasing
		return fmt.Errorf("same user alias forbidden")
	}
	// Does the source binary exist?
	if finfo, err := os.Stat(trustedBinary); err != nil {
		return fmt.Errorf("stat %v failed: %v", trustedBinary, err)
	} else {
		if !finfo.Mode().IsRegular() {
			return fmt.Errorf("%v is not a regular file: %v", finfo.Name(), finfo.Mode())
		}
	}
	// Create base root (/home/user/QubesSync/)if not existing already
	if _, err = setupDir(destRoot, uid, gid); err != nil {
		return err
	}
	// Create vm-root (/home/user/QubesSync/all/) if not existing already
	jail, err = setupDir(filepath.Join(destRoot, "all"), uid, gid)
	if err != nil {
		return fmt.Errorf("setup dir failed: %v", err)
	}
	log.Print("Jail dir ok")
	// All looking good so far, now let's copy the source binary into the
	// future jail
	var (
		newName = fmt.Sprintf("qsync-receive-temp-%d", uint64(rand.Int63()))
		newPath = fmt.Sprintf("%v/%v", jail, newName)
	)
	if err := os.Link(trustedBinary, newPath); err != nil {
		log.Printf("Hard linking failed: %v - trying copy instead.", err)
		// Hard linking fails across fs boundaries, such as
		// /usr/lib/qubes to /home/user/
		// We can do a manual copy instead
		if err = copyFile(trustedBinary, newPath); err != nil {
			return fmt.Errorf("file copying failed: %v", err)
		}
	}
	log.Printf("Copy to %v ok", newPath)
	defer func() {
		if err := os.Remove(newPath); err != nil {
			log.Printf("failed cleaning up %v: %v", newPath, err)
		} else {
			log.Printf("Call done, cleaned up %v ok", newPath)
		}
	}()
	// Set perms so user it can't overwrite itself
	if err := os.Chmod(newPath, 0755); err != nil {
		return fmt.Errorf("chmod op failed: %v", err)
	}
	log.Print("Permissions fixed")
	if err := os.Chdir(destRoot); err != nil {
		return fmt.Errorf("failed chdir: %v", err)
	}
	// I'm actually unsure if this mount/unmount dance actually
	// accomplishes anything ...
	if err := syscall.Mount(".", ".", "", syscall.MS_BIND|syscall.MS_NODEV|syscall.MS_NOEXEC|syscall.MS_NOSUID, ""); err != nil {
		return fmt.Errorf("failed mounting '.': %v", err)
	}
	log.Print("Remount ok. Executing call")
	defer func() {
		if err := syscall.Unmount(".", syscall.MNT_DETACH); err != nil {
			fmt.Fprintf(os.Stderr, "cannot unmount sync directory: %v", err)
		}
	}()
	// Prepare root jail
	cmd := &exec.Cmd{
		Path: fmt.Sprintf("./%v", newName),
		Args: []string{newName},
		Dir:  "/",
		SysProcAttr: &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
			Chroot:     jail,
		},
	}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Run(); err != nil {
		// Or exec failed or the child failed
		if eErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("exit error: %v", eErr.ProcessState.String())
		}
		return fmt.Errorf("failed to run %s as user '%s': %v", newPath, usr.Username, err)
	}
	log.Print("Execution complete")
	return nil
}
