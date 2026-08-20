// Package markdown_folder 从本地 Markdown 目录读取课堂知识库。
package markdown_folder

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Q1mi/kbot/internal/connector"
)

type Connector struct{ Root string }

func New(root string) *Connector { return &Connector{Root: root} }

func (c *Connector) Scan(ctx context.Context) ([]connector.Document, error) {
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	var documents []connector.Document
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		documents = append(documents, connector.Document{
			SourceURI: filepath.ToSlash(relative), Title: titleOf(entry.Name(), string(content)),
			Content: string(content), Checksum: fmt.Sprintf("%x", sum),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan markdown folder: %w", err)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].SourceURI < documents[j].SourceURI })
	return documents, nil
}

func titleOf(filename, content string) string {
	for _, line := range strings.Split(content, "\n") {
		if title := strings.TrimSpace(strings.TrimPrefix(line, "# ")); strings.HasPrefix(strings.TrimSpace(line), "# ") && title != "" {
			return title
		}
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

var _ connector.Connector = (*Connector)(nil)
