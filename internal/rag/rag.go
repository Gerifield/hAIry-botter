package rag

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	"github.com/philippgille/chromem-go"
)

const collectionKey = "rag-content"

// Logic .
type Logic struct {
	logger  *slog.Logger
	ragPath string

	db           *chromem.DB // Database for RAG content
	embeddedDocs int
}

// New .
func New(logger *slog.Logger, ragPath string, embedder chromem.EmbeddingFunc) (*Logic, error) {

	// TODO: we could add an export/import to persist the db
	db := chromem.NewDB()
	_, err := db.CreateCollection(collectionKey, nil, embedder) // Just to make sure the collection exists
	if err != nil {
		logger.Error("failed to create RAG collection", slog.String("collection", collectionKey), slog.String("error", err.Error()))

		return nil, err
	}

	l := &Logic{
		logger:  logger,
		ragPath: ragPath,

		db: db,
	}

	return l, l.loadContent()
}

func (l *Logic) loadContent() error {
	l.logger.Info("started loading rag content", slog.String("path", l.ragPath))
	dir := os.DirFS(l.ragPath)

	coll := l.db.GetCollection(collectionKey, nil) // we can leave the embeddingFunc since it was already set during creation

	ctx := context.Background()
	id := 1
	err := fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk dir %s%s: %w", dir, path, err)
		}
		if d.IsDir() || d.Name() == ".gitkeep" {
			return nil // skip directories and .gitkeep files
		}

		l.logger.Info("loading rag content", slog.String("path", path))

		fmt.Println(path)
		f, err := dir.Open(path)
		if err != nil {
			l.logger.Error("failed to open rag content file", slog.String("path", path), slog.String("error", err.Error()))

			return err
		}
		defer func() { _ = f.Close() }()

		b, err := io.ReadAll(f)
		if err != nil {
			l.logger.Error("failed to read rag content file", slog.String("path", path), slog.String("error", err.Error()))

			return err
		}
		err = coll.AddDocument(ctx, chromem.Document{
			ID:      fmt.Sprintf("%d", id),
			Content: string(b),
		})

		id += 1
		return err
	})

	l.embeddedDocs = id - 1 // Set the number of embedded documents

	l.logger.Info("rag embedding done", slog.Int("num", l.embeddedDocs))

	return err
}

func (l *Logic) Query(ctx context.Context, query string, limit int) ([]chromem.Result, error) {
	l.logger.Info("rag query", slog.String("query", query), slog.Int("limit", limit))

	if l.embeddedDocs == 0 { // No embedded documents available, ignore the query
		l.logger.Warn("rag query called without any embedded documents", slog.String("query", query), slog.Int("limit", limit))

		return make([]chromem.Result, 0), nil
	}

	// Make sure we don't query more than we have embedded
	if limit > l.embeddedDocs {
		limit = l.embeddedDocs
	}

	coll := l.db.GetCollection(collectionKey, nil)
	res, err := coll.Query(ctx, query, limit, nil, nil)
	if err != nil {
		l.logger.Error("failed to query rag content", slog.String("query", query), slog.Int("limit", limit), slog.String("error", err.Error()))

		return nil, err
	}

	l.logger.Info("rag query done", slog.Int("num_results", len(res)))

	return res, nil
}
