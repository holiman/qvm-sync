package packer

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"time"
)

const (
	useTempFile = true
	LongMax     = 9223372036854775807
	ByteLimit   = 0
	verbose     = true
	filesLimit  = -1
)

var (
	totalBytes = uint64(0)
	totalFiles = uint64(0)
)

/*

void fix_times_and_perms(struct file_header *untrusted_hdr,
        const char *untrusted_name)
{
    struct timeval times[2] =
    {
        {untrusted_hdr->atime, untrusted_hdr->atime_nsec / 1000},
        {untrusted_hdr->mtime, untrusted_hdr->mtime_nsec / 1000}
    };
    if (chmod(untrusted_name, untrusted_hdr->mode & 07777))  // safe because of chroot
        do_exit(errno, untrusted_name);
    if (utimes(untrusted_name, times))  //as above
        do_exit(errno, untrusted_name);
}
*/

func fixTimesAndPerms(untrustedHeader *fileHeader, untrustedName string) error {
	if err := os.Chmod(untrustedName, os.FileMode(untrustedHeader.mode&07777)); err != nil {
		return err
	}

	atime := time.Unix(int64(untrustedHeader.atime), int64(untrustedHeader.atimeNsec))
	mtime := time.Unix(int64(untrustedHeader.mtime), int64(untrustedHeader.mtimeNsec))
	return os.Chtimes(untrustedName, atime, mtime)
}

//
//void process_one_file_reg(struct file_header *untrusted_hdr,
//        const char *untrusted_name)
//{
//    int ret;
//    int fdout = -1;
//
//    /* make the file inaccessible until fully written */
//    if (use_tmpfile) {
//        fdout = open(".", O_WRONLY | O_TMPFILE, 0700);
//        if (fdout < 0) {
//            if (errno==ENOENT || /* most likely, kernel too old for O_TMPFILE */
//                    errno==EOPNOTSUPP) /* filesystem has no support for O_TMPFILE */
//                use_tmpfile = 0;
//            else
//                do_exit(errno, untrusted_name);
//        }
//    }
//    if (fdout < 0)
//        fdout = open(untrusted_name, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0000); /* safe because of chroot */
//    if (fdout < 0)
//        do_exit(errno, untrusted_name);
//    /* sizes are signed elsewhere */
//    if (untrusted_hdr->filelen > LLONG_MAX || (bytes_limit && untrusted_hdr->filelen > bytes_limit))
//        do_exit(EDQUOT, untrusted_name);
//    if (bytes_limit && total_bytes > bytes_limit - untrusted_hdr->filelen)
//        do_exit(EDQUOT, untrusted_name);
//    total_bytes += untrusted_hdr->filelen;
//    ret = copy_file(fdout, 0, untrusted_hdr->filelen, &crc32_sum);
//    if (ret != COPY_FILE_OK) {
//        if (ret == COPY_FILE_READ_EOF
//                || ret == COPY_FILE_READ_ERROR)
//            do_exit(LEGAL_EOF, untrusted_name); // hopefully remote will produce error message
//        else
//            do_exit(errno, untrusted_name);
//    }
//    if (use_tmpfile) {
//        char fd_str[7];
//        snprintf(fd_str, sizeof(fd_str), "%d", fdout);
//        if (linkat(procdir_fd, fd_str, AT_FDCWD, untrusted_name, AT_SYMLINK_FOLLOW) < 0)
//            do_exit(errno, untrusted_name);
//    }
//    close(fdout);
//    fix_times_and_perms(untrusted_hdr, untrusted_name);
//}
//
func receiveRegularFile(untrustedHeader *fileHeader, untrustedName string, input io.Reader) error {

	// Check sizes
	fLen := untrustedHeader.fileLen
	if fLen > LongMax || (ByteLimit != 0 && fLen > ByteLimit) {
		return fmt.Errorf("file too large, %d", fLen)
	}
	if ByteLimit != 0 && totalBytes > uint64(ByteLimit)-fLen {
		return fmt.Errorf("file too large, %d", fLen)
	}
	totalBytes += fLen
	var (
		fdOut *os.File
		err   error
	)
	if useTempFile {
		fdOut, err = ioutil.TempFile(".", "qvm-*")
	} else {
		fdOut, err = os.OpenFile(untrustedName, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0)
	}
	if err != nil {
		return err
	}
	defer fdOut.Close()
	if err := CopyFile(input, fdOut, int(fLen)); err != nil {
		return err
	}
	if useTempFile {
		// Regardless of error, clean up tempfile
		defer os.Remove(fdOut.Name())
		if err := os.Link(fdOut.Name(), untrustedName); err != nil {
			return err
		}
	}
	return fixTimesAndPerms(untrustedHeader, untrustedName)
}

//
//void process_one_file_dir(struct file_header *untrusted_hdr,
//        const char *untrusted_name)
//{
//    // fix perms only when the directory is sent for the second time
//    // it allows to transfer r.x directory contents, as we create it rwx initially
//    struct stat buf;
//    if (!mkdir(untrusted_name, 0700)) /* safe because of chroot */
//        return;
//    if (errno != EEXIST)
//        do_exit(errno, untrusted_name);
//    if (stat(untrusted_name,&buf) < 0)
//        do_exit(errno, untrusted_name);
//    total_bytes += buf.st_size;
//    /* size accumulated after the fact, so don't check limit here */
//    fix_times_and_perms(untrusted_hdr, untrusted_name);
//}
//
func receiveDir(untrustedHeader *fileHeader, untrustedName string, input io.Reader) error {

	if _, err := os.Stat(untrustedName); err == nil {
		// directory already exists
	}
	// safe because of chroot
	if err := os.Mkdir(untrustedName, 0700); err != nil {
		if !os.IsExist(err) {
			return err
		}
		// Already exists, that's fine, it means it's the second time
		// around we hit this (dir end)
		// we fix the perms after we're done with it (second time around)
		return fixTimesAndPerms(untrustedHeader, untrustedName)
	} else {
		// Dir created, exit
		return nil
	}
}

//
//void process_one_file_link(struct file_header *untrusted_hdr,
//        const char *untrusted_name)
//{
//    char untrusted_content[MAX_PATH_LENGTH];
//    unsigned int filelen;
//    if (untrusted_hdr->filelen > MAX_PATH_LENGTH - 1)
//        do_exit(ENAMETOOLONG, untrusted_name);
//    filelen = untrusted_hdr->filelen; /* sanitized above */
//    total_bytes += filelen;
//    if (bytes_limit && total_bytes > bytes_limit)
//        do_exit(EDQUOT, untrusted_name);
//    if (!read_all_with_crc(0, untrusted_content, filelen))
//        do_exit(LEGAL_EOF, untrusted_name); // hopefully remote has produced error message
//    untrusted_content[filelen] = 0;
//    if (symlink(untrusted_content, untrusted_name)) /* safe because of chroot */
//        do_exit(errno, untrusted_name);
//
//}
//

func receiveSymlink(untrustedHeader *fileHeader, untrustedName string, input io.Reader) error {
	fLen := untrustedHeader.fileLen
	if fLen > MaxPathLength-1 {
		return fmt.Errorf("file name too long (%d characters)", fLen)
	}
	if ByteLimit != 0 && totalBytes > uint64(ByteLimit)-fLen {
		return fmt.Errorf("file too large, %d", fLen)
	}
	totalBytes += fLen
	// a symlink should be small enough to not use CopyFile (buffered)
	buf := make([]byte, fLen)
	if n, _ := input.Read(buf); uint64(n) != fLen {
		return fmt.Errorf("read err, wanted %d, got only %d", fLen, n)
	}
	untrustedContent := string(buf[:fLen-1])
	return os.Symlink(untrustedContent, untrustedName)
}

//
//
//void process_one_file(struct file_header *untrusted_hdr)
//{
//    unsigned int namelen;
//    if (untrusted_hdr->namelen > MAX_PATH_LENGTH - 1)
//        do_exit(ENAMETOOLONG, NULL); /* filename too long so not received at all */
//    namelen = untrusted_hdr->namelen; /* sanitized above */
//    if (!read_all_with_crc(0, untrusted_namebuf, namelen))
//        do_exit(LEGAL_EOF, NULL); // hopefully remote has produced error message
//    untrusted_namebuf[namelen] = 0;
//    if (S_ISREG(untrusted_hdr->mode))
//        process_one_file_reg(untrusted_hdr, untrusted_namebuf);
//    else if (S_ISLNK(untrusted_hdr->mode))
//        process_one_file_link(untrusted_hdr, untrusted_namebuf);
//    else if (S_ISDIR(untrusted_hdr->mode))
//        process_one_file_dir(untrusted_hdr, untrusted_namebuf);
//    else
//        do_exit(EINVAL, untrusted_namebuf);
//    if (verbose && !S_ISDIR(untrusted_hdr->mode))
//        fprintf(stderr, "%s\n", untrusted_namebuf);
//}
//
func processFile(hdr *fileHeader, input io.Reader) (string, error) {
	nLen := hdr.nameLen
	if nLen > MaxPathLength-1 {
		return "", fmt.Errorf("file name too long (%d characters)", nLen)
	}
	nBuf := make([]byte, nLen)
	if n, _ := input.Read(nBuf); n != int(nLen) {
		return "", fmt.Errorf("read err, wanted %d, got only %d", nLen, n)
	}
	if nBuf[nLen-1] != 0 {
		return "", fmt.Errorf("expected NULL-terminated string")
	}
	var (
		name = string(nBuf[:nLen-1])
		mode = os.FileMode(hdr.mode)
		err  error
	)
	switch {
	case mode.IsRegular():
		err = receiveRegularFile(hdr, name, input)
	case mode.IsDir():
		err = receiveDir(hdr, name, input)
	case mode&os.ModeSymlink != 0:
		err = receiveSymlink(hdr, name, input)
	default:
		return "", fmt.Errorf("unknown file mode %d", hdr.mode)
	}
	if verbose && len(name) > 0 {
		log.Printf("%s", name)
	}
	return name, err

}

//
//int do_unpack(void)
//{
//    struct file_header untrusted_hdr;
//#ifdef HAVE_SYNCFS
//    int cwd_fd;
//    int saved_errno;
//#endif
//
//    total_bytes = total_files = 0;
//    /* initialize checksum */
//    crc32_sum = 0;
//    while (read_all_with_crc(0, &untrusted_hdr, sizeof untrusted_hdr)) {
//        /* check for end of transfer marker */
//        if (untrusted_hdr.namelen == 0) {
//            errno = 0;
//            break;
//        }
//        total_files++;
//        if (files_limit && total_files > files_limit)
//            do_exit(EDQUOT, untrusted_namebuf);
//        process_one_file(&untrusted_hdr);
//    }
//
//#ifdef HAVE_SYNCFS
//    saved_errno = errno;
//    cwd_fd = open(".", O_RDONLY);
//    if (cwd_fd >= 0 && syncfs(cwd_fd) == 0 && close(cwd_fd) == 0)
//        errno = saved_errno;
//#else
//    sync();
//#endif
//
//    send_status_and_crc(errno, untrusted_namebuf);
//    return errno;
//}
//
func DoUnpack(input io.Reader, out io.Writer) error {
	totalBytes = 0
	totalFiles = 0
	var lastName string
	for {
		hdr := new(fileHeader)
		if err := hdr.unMarshallBinary(input); err != nil {
			return err
		}
		// Check for end of transfer marker
		if hdr.nameLen == 0 {
			break
		}
		totalFiles++
		if filesLimit > 0 && int(totalFiles) > filesLimit {
			return fmt.Errorf("number of files (%d) exceeded limit (%d)", totalFiles, filesLimit)
		}
		if name, err := processFile(hdr, input); err != nil {
			return err
		} else {
			lastName = name
		}
	}
	return sendStatusAndCrc(0, lastName, out)
}

//
//void send_status_and_crc(int code, const char *last_filename) {
//    struct result_header hdr;
//    struct result_header_ext hdr_ext;
//    int saved_errno;
//
//    saved_errno = errno;
//    hdr.error_code = code;
//    hdr._pad = 0;
//    hdr.crc32 = crc32_sum;
//    if (!write_all(1, &hdr, sizeof(hdr)))
//        perror("write status");
//    if (last_filename) {
//        hdr_ext.last_namelen = strlen(last_filename);
//        if (!write_all(1, &hdr_ext, sizeof(hdr_ext)))
//            perror("write status ext");
//        if (!write_all(1, last_filename, hdr_ext.last_namelen))
//            perror("write last_filename");
//    }
//    errno = saved_errno;
//}
//

func sendStatusAndCrc(code int, lastFilename string, output io.Writer) error {
	result := resultHeader{
		errorCode: uint32(code),
		pad:       0,
		crc32:     0,
	}
	if err := result.marshallBinary(output); err != nil {
		return err
	}

	if len(lastFilename) > 0 {
		extension := resultHeaderExt{
			lastNameLen: uint32(len(lastFilename)),
			lastName:    lastFilename,
		}
		if err := extension.marshallBinary(output); err != nil {
			return fmt.Errorf("failed sending result extension: %v", err)
		}
	}
	return nil
}
