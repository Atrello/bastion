package agent

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
	"strings"
	"syscall"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// Backups of a compose stack: one .tar.gz holding its folder (stack/...: the compose file, .env and anything
// kept beside them, like a ./data folder) and every volume Docker made for it (volumes/<name>/...), with
// their owners and permissions, so a database comes back as it was. The stack can be stopped while it's
// copied, so a database is copied whole rather than mid-write. Restoring empties the folder and each volume
// in the backup and puts the backup's copy back, then starts the stack again if it was running.
// Kept in the backups folder (stacks/<name>/), or in S3 like game backups.

func stackBackupDir(stack string) string {
	return filepath.Join(filepath.Dir(backupDir(0)), "stacks", stack)
}

func stackBackup(ctx context.Context, stacksDir string, a protocol.StackBackupArgs, t *tracker) (*protocol.GameBackupResult, error) {
	if !stackName.MatchString(a.Stack) || !backupName.MatchString(a.Name) {
		return nil, errors.New("that isn't a stack backup")
	}
	file := filepath.Join(stackBackupDir(a.Stack), a.Name)
	switch a.Op {
	case "make":
		size, err := makeStackBackup(ctx, stacksDir, a, file, t)
		if err == nil && a.S3 != nil {
			err = s3Upload(ctx, a.S3, file)
			_ = os.Remove(file)
		}
		return &protocol.GameBackupResult{Size: size}, err
	case "restore":
		if a.S3 != nil {
			if err := os.MkdirAll(stackBackupDir(a.Stack), 0o700); err != nil {
				return nil, err
			}
			return &protocol.GameBackupResult{}, withS3Copy(ctx, a.S3, file, func(f string) error { return restoreStackBackup(ctx, stacksDir, a, f, t) })
		}
		return &protocol.GameBackupResult{}, restoreStackBackup(ctx, stacksDir, a, file, t)
	case "delete":
		if a.S3 != nil {
			return &protocol.GameBackupResult{}, s3Delete(ctx, a.S3)
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return &protocol.GameBackupResult{}, nil
	case "download":
		if a.S3 != nil {
			data, size, err := s3Piece(ctx, a.S3, a.Offset)
			return &protocol.GameBackupResult{Data: data, Size: size}, err
		}
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

// stackVolumes is the volumes Docker made for a stack (labelled with its project), with where each is kept.
func stackVolumes(ctx context.Context, stack string) (map[string]string, error) {
	out, err := docker(ctx, "", "volume", "ls", "--filter", "label=com.docker.compose.project="+stack, "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	volumes := map[string]string{}
	for _, name := range strings.Fields(out) {
		mount, err := docker(ctx, "", "volume", "inspect", "--format", "{{.Mountpoint}}", name)
		if err != nil {
			return nil, err
		}
		if mount = strings.TrimSpace(mount); mount != "" {
			volumes[name] = mount
		}
	}
	return volumes, nil
}

// pauseStack stops a running stack for the length of a backup or restore, and gives what starts it again.
func pauseStack(ctx context.Context, s *stack) (func(), error) {
	running, _ := docker(ctx, s.dir, "compose", "-p", s.name, "-f", s.configFile, "ps", "--quiet", "--status", "running")
	if strings.TrimSpace(running) == "" {
		return func() {}, nil
	}
	if _, err := docker(ctx, s.dir, "compose", "-p", s.name, "-f", s.configFile, "stop"); err != nil {
		return nil, fmt.Errorf("stopping it first: %w", err)
	}
	return func() {
		// Started again whatever happened, with a context of its own: a cancelled backup still restarts it.
		start, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, _ = docker(start, s.dir, "compose", "-p", s.name, "-f", s.configFile, "up", "-d")
	}, nil
}

func makeStackBackup(ctx context.Context, stacksDir string, a protocol.StackBackupArgs, file string, t *tracker) (int64, error) {
	s, err := findStack(ctx, stacksDir, a.Stack)
	if err != nil {
		return 0, err
	}
	volumes, err := stackVolumes(ctx, s.name)
	if err != nil {
		return 0, err
	}
	if a.Stop {
		resume, err := pauseStack(ctx, s)
		if err != nil {
			return 0, err
		}
		defer resume()
	}
	if t != nil {
		t.total.Store(treeSize(s.dir))
		for _, mount := range volumes {
			t.total.Add(treeSize(mount))
		}
	}
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
	err = addTree(tw, s.dir, "stack", t)
	for name, mount := range volumes {
		if err != nil {
			break
		}
		err = addTree(tw, mount, "volumes/"+name, t)
	}
	for _, c := range []io.Closer{tw, gz, out} {
		if cerr := c.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		if t.stopped() != nil {
			return 0, errors.New("stopped: the part-made backup was removed")
		}
		return 0, fmt.Errorf("the backup failed: %w", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		return 0, err
	}
	return info.Size(), os.Rename(tmp, file)
}

// treeSize is the size of the files under dir, for saying how far a backup has got.
func treeSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// addTree adds dir to the archive under prefix: folders, files and links, with owners and permissions.
// It doesn't follow links or leave dir's disk.
func addTree(tw *tar.Writer, dir, prefix string, t *tracker) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := t.stopped(); err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if !mode.IsDir() && !mode.IsRegular() && mode&fs.ModeSymlink == 0 {
			return nil // sockets, pipes and devices aren't data
		}
		link := ""
		if mode&fs.ModeSymlink != 0 {
			if link, err = root.Readlink(p); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = path.Join(prefix, p)
		if mode.IsDir() {
			hdr.Name += "/"
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			hdr.Uid, hdr.Gid = int(st.Uid), int(st.Gid)
		}
		hdr.Uname, hdr.Gname = "", ""
		if err := tw.WriteHeader(hdr); err != nil || !mode.IsRegular() {
			return err
		}
		f, err := root.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.CopyN(tw, t.reader(f), info.Size())
		return err
	})
}

func restoreStackBackup(ctx context.Context, stacksDir string, a protocol.StackBackupArgs, file string, t *tracker) error {
	in, err := os.Open(file)
	if err != nil {
		return errors.New("that backup isn't there")
	}
	defer in.Close()
	if t != nil {
		if info, err := in.Stat(); err == nil {
			t.total.Store(info.Size())
		}
	}

	// Where each part goes: the stack's folder (as it is now, or a new one in the stacks folder), and each
	// volume (made again if it's gone).
	dir := filepath.Join(stacksDir, a.Stack)
	s, err := findStack(ctx, stacksDir, a.Stack)
	if err == nil {
		dir = s.dir
		resume, err := pauseStack(ctx, s)
		if err != nil {
			return err
		}
		defer resume()
	}
	volumes, err := stackVolumes(ctx, a.Stack)
	if err != nil {
		return err
	}

	gz, err := gzip.NewReader(t.reader(in))
	if err != nil {
		return errors.New("that backup can't be read")
	}
	tr := tar.NewReader(gz)
	roots := map[string]*os.Root{}
	defer func() {
		for _, r := range roots {
			r.Close()
		}
	}()
	// rootFor empties and opens a part the first time the backup reaches it.
	rootFor := func(part string) (*os.Root, error) {
		if r := roots[part]; r != nil {
			return r, nil
		}
		target := dir
		if name, ok := strings.CutPrefix(part, "volumes/"); ok {
			if !volumeName.MatchString(name) {
				return nil, fmt.Errorf("%s isn't a volume's name", name)
			}
			mount, ok := volumes[name]
			if !ok {
				if _, err := docker(ctx, "", "volume", "create", "--label", "com.docker.compose.project="+a.Stack,
					"--label", "com.docker.compose.volume="+strings.TrimPrefix(name, a.Stack+"_"), name); err != nil {
					return nil, fmt.Errorf("making the volume %s: %w", name, err)
				}
				out, err := docker(ctx, "", "volume", "inspect", "--format", "{{.Mountpoint}}", name)
				if err != nil {
					return nil, err
				}
				mount = strings.TrimSpace(out)
			}
			target = mount
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, err
		}
		r, err := os.OpenRoot(target)
		if err != nil {
			return nil, err
		}
		if err := emptyRoot(r); err != nil {
			r.Close()
			return nil, err
		}
		roots[part] = r
		return r, nil
	}

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if t.stopped() != nil {
				return errors.New("stopped part-way: what was restored so far stays")
			}
			return fmt.Errorf("that backup can't be read: %w", err)
		}
		part, rel := splitPart(hdr.Name)
		if part == "" {
			continue
		}
		r, err := rootFor(part)
		if err != nil {
			return err
		}
		if err := restoreEntry(r, rel, hdr, tr); err != nil {
			return fmt.Errorf("restoring %s: %w", hdr.Name, err)
		}
	}
}

var volumeName = stackName // Docker's own volume names are wider, but compose's are <project>_<name>

// splitPart splits "stack/data/a.db" into ("stack", "data/a.db") and "volumes/x_db/base/1" into
// ("volumes/x_db", "base/1"); anything else is no part of a stack backup.
func splitPart(name string) (part, rel string) {
	name = strings.TrimSuffix(name, "/")
	if rest, ok := strings.CutPrefix(name, "stack"); ok && (rest == "" || rest[0] == '/') {
		return "stack", strings.TrimPrefix(rest, "/")
	}
	if rest, ok := strings.CutPrefix(name, "volumes/"); ok {
		vol, inner, _ := strings.Cut(rest, "/")
		if vol != "" {
			return "volumes/" + vol, inner
		}
	}
	return "", ""
}

// emptyRoot removes everything inside r, leaving r itself.
func emptyRoot(r *os.Root) error {
	top, err := r.Open(".")
	if err != nil {
		return err
	}
	entries, _ := top.ReadDir(-1)
	top.Close()
	for _, e := range entries {
		if err := r.RemoveAll(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// restoreEntry writes one entry from the archive inside r: a folder, a file or a link, with its owner and
// permissions. os.Root keeps every path, and every link it follows, inside r.
func restoreEntry(r *os.Root, rel string, hdr *tar.Header, body io.Reader) error {
	if rel == "" {
		if hdr.Typeflag == tar.TypeDir {
			_ = r.Chmod(".", fs.FileMode(hdr.Mode).Perm())
			_ = r.Lchown(".", hdr.Uid, hdr.Gid)
		}
		return nil
	}
	rel = insidePath(rel)
	if rel == "." {
		return nil
	}
	if parent := path.Dir(rel); parent != "." {
		if err := r.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	mode := fs.FileMode(hdr.Mode).Perm()
	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := r.MkdirAll(rel, mode); err != nil {
			return err
		}
		_ = r.Chmod(rel, mode)
	case tar.TypeReg:
		f, err := r.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, body)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		_ = r.Chmod(rel, mode)
	case tar.TypeSymlink:
		_ = r.Remove(rel)
		if err := r.Symlink(hdr.Linkname, rel); err != nil {
			return err
		}
	default:
		return nil
	}
	_ = r.Lchown(rel, hdr.Uid, hdr.Gid)
	return nil
}
