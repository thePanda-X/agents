package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agents/internal/harness"
)

func ReadAttachment(path string) (harness.Attachment, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return harness.Attachment{}, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return harness.Attachment{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return harness.Attachment{}, err
	}
	if !info.Mode().IsRegular() {
		return harness.Attachment{}, errors.New("attachment must be a regular file")
	}
	if info.Size() > 8<<20 {
		return harness.Attachment{}, errors.New("attachment exceeds 8 MiB")
	}
	typeName := mime.TypeByExtension(filepath.Ext(absolute))
	if typeName == "" {
		header := make([]byte, 512)
		n, _ := file.Read(header)
		typeName = http.DetectContentType(header[:n])
		if _, err := file.Seek(0, 0); err != nil {
			return harness.Attachment{}, err
		}
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
	a := harness.Attachment{Path: absolute, Name: filepath.Base(absolute), URI: uri, MIME: typeName, Image: strings.HasPrefix(typeName, "image/")}
	if a.Image {
		data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
		if err != nil {
			return harness.Attachment{}, err
		}
		if len(data) > 8<<20 {
			return harness.Attachment{}, errors.New("attachment grew beyond 8 MiB")
		}
		a.Data = data
	}
	return a, nil
}

// IndexFiles honors gitignore in repositories and uses a bounded fallback
// elsewhere. It never follows directory symlinks or scans dependency caches.
func IndexFiles(ctx context.Context, directory string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = directory
	if output, err := cmd.Output(); err == nil {
		files := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
		if len(files) == 1 && files[0] == "" {
			return nil, nil
		}
		return files[:min(len(files), 5000)], nil
	}
	var files []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor", ".cache":
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			rel, err := filepath.Rel(directory, path)
			if err == nil {
				files = append(files, rel)
			}
		}
		if len(files) >= 5000 {
			return fs.SkipAll
		}
		return nil
	})
	return files, err
}
