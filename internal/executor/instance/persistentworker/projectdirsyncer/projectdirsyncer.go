package projectdirsyncer

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cirruslabs/cirrus-cli/internal/executor/platform"
	"golang.org/x/crypto/ssh"
)

// SyncProjectDir streams the project directory into the guest as a single
// tar archive piped into a remote tar over the SSH exec channel.
//
// A single stream replaces the previous per-file SFTP copy: one round trip
// instead of one per file, and modes/symlinks survive (SFTP Create() reset
// modes and skipped symlinks entirely). Only tar is required in the guest
// (present in FreeBSD/macOS base and virtually all Linux images), unlike
// rsync, and no SFTP subsystem is needed.
func SyncProjectDir(dir string, sshClient *ssh.Client) error {
	remoteDir := platform.NewUnix().GenericWorkingDir()

	session, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}

	var stderr bytes.Buffer
	session.Stderr = &stderr

	// -p preserves modes (the whole point: scripts stay executable);
	// the base directory is created first since tar -C requires it.
	command := "mkdir -p " + shQuote(remoteDir) + " && tar -xpf - -C " + shQuote(remoteDir)
	if err := session.Start(command); err != nil {
		return fmt.Errorf("failed to start remote tar: %v", err)
	}

	writeErr := writeTarArchive(stdin, dir)
	_ = stdin.Close()
	waitErr := session.Wait()

	if writeErr != nil {
		return fmt.Errorf("failed to stream project directory: %v", writeErr)
	}
	if waitErr != nil {
		return fmt.Errorf("remote tar failed: %v: %s", waitErr, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// writeTarArchive streams dir (without dir itself) as a tar archive.
func writeTarArchive(w io.Writer, dir string) error {
	tw := tar.NewWriter(w)

	walkErr := filepath.Walk(dir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relativePath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if relativePath == "." {
			// The base directory is created remotely up front.
			return nil
		}

		linkTarget := ""
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return err
			}
		} else if !fileInfo.Mode().IsDir() && !fileInfo.Mode().IsRegular() {
			// Skip sockets, devices, etc., as before.
			return nil
		}

		header, err := tar.FileInfoHeader(fileInfo, linkTarget)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relativePath)
		if fileInfo.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		// Ownership is meaningless across the VM boundary and only
		// invites warnings from picky tar implementations.
		header.Uid, header.Gid, header.Uname, header.Gname = 0, 0, "", ""

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if fileInfo.Mode().IsRegular() {
			localFile, err := os.Open(path)
			if err != nil {
				return err
			}
			defer localFile.Close()

			if _, err := io.Copy(tw, localFile); err != nil {
				return err
			}
		}

		return nil
	})

	if err := tw.Close(); err != nil && walkErr == nil {
		walkErr = err
	}

	return walkErr
}

// shQuote wraps s for POSIX sh single-quoting.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
