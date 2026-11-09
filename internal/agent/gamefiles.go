package agent

// A game's file manager and backups. Every path goes through os.Root on the game's folder, which refuses
// anything that would leave it: "..", absolute paths, and links pointing out of it.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

const (
	maxEditable = 2 << 20 // a text file opened in the editor
	chunkSize   = 2 << 20 // a piece of an upload or download: under the websocket's 4 MB limit as base64
)

func backupDir(id int64) string {
	base := os.Getenv("BASTION_AGENT_BACKUPS_DIR")
	if base == "" {
		base = "/var/lib/bastion/backups"
	}
	return filepath.Join(base, strconv.FormatInt(id, 10))
}

var backupName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}\.tar\.gz$`)

// insidePath turns what the panel sends ("/world/level.dat", "plugins/") into a path os.Root takes.
func insidePath(p string) string {
	p = path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
	if p == "/" {
		return "."
	}
	return p[1:]
}

func openGame(id int64) (*os.Root, error) {
	if id <= 0 {
		return nil, errors.New("that isn't a game server")
	}
	root, err := os.OpenRoot(gameDir(id))
	if err != nil {
		return nil, errors.New("it isn't installed on this server")
	}
	return root, nil
}

func gameFiles(a protocol.GameFileArgs) (*protocol.GameFileResult, error) {
	if a.Op == "install-log" {
		data, err := installLog(a.ID)
		return &protocol.GameFileResult{Data: data}, err
	}
	root, err := openGame(a.ID)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	p := insidePath(a.Path)
	switch a.Op {
	case "list":
		f, err := root.Open(p)
		if err != nil {
			return nil, errors.New("there's no folder there")
		}
		defer f.Close()
		entries, err := f.ReadDir(-1)
		if err != nil {
			return nil, errors.New("that isn't a folder")
		}
		files := make([]protocol.GameFile, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, protocol.GameFile{Name: e.Name(), Dir: e.IsDir(), Size: info.Size(), Modified: info.ModTime().UTC().Format(time.RFC3339)})
		}
		return &protocol.GameFileResult{Files: files}, nil
	case "read":
		info, err := root.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("that isn't a file")
		}
		if info.Size() > maxEditable {
			return nil, errors.New("that file is too big to edit here: download it instead")
		}
		data, err := root.ReadFile(p)
		return &protocol.GameFileResult{Data: data, Size: info.Size()}, err
	case "write", "append":
		if p == "." {
			return nil, errors.New("that needs a file name")
		}
		if len(a.Data) > chunkSize {
			return nil, errors.New("too much at once")
		}
		flags := os.O_WRONLY | os.O_CREATE
		if a.Op == "write" || a.Offset == 0 {
			flags |= os.O_TRUNC
		}
		f, err := root.OpenFile(p, flags, 0o644)
		if err != nil {
			return nil, fmt.Errorf("it can't be written: %w", err)
		}
		_, err = f.WriteAt(a.Data, a.Offset)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		_ = root.Lchown(p, gameUID, gameGID) // the agent runs as root, so this only fails in tests
		return &protocol.GameFileResult{}, err
	case "download":
		f, err := root.Open(p)
		if err != nil {
			return nil, errors.New("that isn't a file")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("that isn't a file")
		}
		buf := make([]byte, min(chunkSize, max(0, info.Size()-a.Offset)))
		n, err := f.ReadAt(buf, a.Offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		return &protocol.GameFileResult{Data: buf[:n], Size: info.Size()}, nil
	case "mkdir":
		if err := root.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
		_ = root.Lchown(p, gameUID, gameGID)
		return &protocol.GameFileResult{}, nil
	case "rename":
		to := insidePath(a.To)
		if p == "." || to == "." {
			return nil, errors.New("the game's folder itself can't be moved")
		}
		return &protocol.GameFileResult{}, root.Rename(p, to)
	case "delete":
		if p == "." {
			return nil, errors.New("the game's folder itself can't be deleted")
		}
		return &protocol.GameFileResult{}, root.RemoveAll(p)
	}
	return nil, errors.New("unknown file operation " + a.Op)
}

func gameBackup(ctx context.Context, a protocol.GameBackupArgs) (*protocol.GameBackupResult, error) {
	if a.ID <= 0 || !backupName.MatchString(a.Name) {
		return nil, errors.New("that isn't a backup")
	}
	file := filepath.Join(backupDir(a.ID), a.Name)
	switch a.Op {
	case "make":
		size, err := makeBackup(a.ID, file)
		return &protocol.GameBackupResult{Size: size}, err
	case "restore":
		if running, _ := gameRunning(ctx, a.ID); running {
			return nil, errors.New("stop it before restoring a backup")
		}
		return &protocol.GameBackupResult{}, restoreBackup(a.ID, file)
	case "delete":
		return &protocol.GameBackupResult{}, os.Remove(file)
	case "download":
		f, err := os.Open(file)
		if err != nil {
			return nil, errors.New("that backup isn't there")
		}
		defer f.Close()
		info, _ := f.Stat()
		buf := make([]byte, min(chunkSize, max(0, info.Size()-a.Offset)))
		n, err := f.ReadAt(buf, a.Offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		return &protocol.GameBackupResult{Data: buf[:n], Size: info.Size()}, nil
	}
	return nil, errors.New("unknown backup operation " + a.Op)
}

// makeBackup writes the game's folder to a .tar.gz: plain files and folders only.
func makeBackup(id int64, file string) (int64, error) {
	root, err := openGame(id)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return 0, err
	}
	tmp := file + ".part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp)
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." || !(d.IsDir() || d.Type().IsRegular()) {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name, hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = p, 0, 0, "", ""
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil || d.IsDir() {
			return err
		}
		f, err := root.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.CopyN(tw, f, info.Size())
		return err
	})
	for _, closer := range []io.Closer{tw, gz, out} {
		if cerr := closer.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return 0, fmt.Errorf("the backup failed: %w", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		return 0, err
	}
	return info.Size(), os.Rename(tmp, file)
}

// restoreBackup empties the game's folder and unpacks the backup into it.
func restoreBackup(id int64, file string) error {
	in, err := os.Open(file)
	if err != nil {
		return errors.New("that backup isn't there")
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return errors.New("that backup can't be read")
	}
	root, err := openGame(id)
	if err != nil {
		return err
	}
	defer root.Close()
	top, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, _ := top.ReadDir(-1)
	top.Close()
	for _, e := range entries {
		if err := root.RemoveAll(e.Name()); err != nil {
			return err
		}
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("that backup can't be read: %w", err)
		}
		p := insidePath(hdr.Name)
		if p == "." {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			err = root.MkdirAll(p, 0o755)
		case tar.TypeReg:
			if dir := path.Dir(p); dir != "." {
				if err = root.MkdirAll(dir, 0o755); err != nil {
					break
				}
			}
			var f *os.File
			if f, err = root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644); err == nil {
				_, err = io.Copy(f, tr)
				if cerr := f.Close(); err == nil {
					err = cerr
				}
			}
		default:
			continue // links and devices aren't restored
		}
		if err != nil {
			return fmt.Errorf("restoring %s: %w", p, err)
		}
		_ = root.Lchown(p, gameUID, gameGID)
	}
}
