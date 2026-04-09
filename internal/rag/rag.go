package rag

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/philippgille/chromem-go"
)

const (
	collectionKey = "rag-content"
	dbSaveName    = "database.db"
)

// Logic .
type Logic struct {
	logger  *slog.Logger
	ragPath string

	db           *chromem.DB // Database for RAG content
	embeddedDocs int
	embedFn      chromem.EmbeddingFunc
}

// New .
func New(logger *slog.Logger, ragPath string, embedder chromem.EmbeddingFunc) (*Logic, error) {
	logger.Info("try to load RAG db")
	db, err := loadSavedDB(ragPath)
	if err != nil {
		// Doesn't exist or failed, recreate it
		logger.Info("init new RAG db")
		db = chromem.NewDB()
		_, err := db.CreateCollection(collectionKey, nil, embedder) // Just to make sure the collection exists
		if err != nil {
			logger.Error("failed to create RAG collection", slog.String("collection", collectionKey), slog.String("error", err.Error()))

			return nil, err
		}
	}

	l := &Logic{
		logger:  logger,
		ragPath: ragPath,

		db:      db,
		embedFn: embedder,
	}

	return l, l.loadContent()
}

func (l *Logic) Close() error {
	return l.db.ExportToFile(path.Join(l.ragPath, dbSaveName), true, "")
}

func loadSavedDB(dbPath string) (*chromem.DB, error) {
	db := chromem.NewDB()
	err := db.ImportFromFile(path.Join(dbPath, dbSaveName), "")
	if err != nil {
		return nil, err
	}

	return db, nil
}

func (l *Logic) loadContent() error {
	l.logger.Info("started loading rag content", slog.String("path", l.ragPath))
	dir := os.DirFS(l.ragPath)

	// we need to set embed function since we might have loaded an existing db
	coll := l.db.GetCollection(collectionKey, l.embedFn)

	ctx := context.Background()
	id := 1
	err := fs.WalkDir(dir, ".", func(fName string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk dir %s%s: %w", dir, fName, err)
		}
		if d.IsDir() || d.Name() == ".gitkeep" || strings.HasSuffix(fName, ".loaded") || d.Name() == dbSaveName {
			return nil // skip directories, .gitkeep, loaded files and the db file itself
		}

		l.logger.Info("loading rag content", slog.String("file", fName))

		f, err := dir.Open(fName)
		if err != nil {
			l.logger.Error("failed to open rag content file", slog.String("file", fName), slog.String("error", err.Error()))

			return err
		}
		defer func() { _ = f.Close() }()

		b, err := io.ReadAll(f)
		if err != nil {
			l.logger.Error("failed to read rag content file", slog.String("file", fName), slog.String("error", err.Error()))

			return err
		}
		err = coll.AddDocument(ctx, chromem.Document{
			ID:      fmt.Sprintf("%d", id),
			Content: string(b),
		})
		if err != nil {
			return err
		}

		fullPath := path.Join(l.ragPath, fName)
		err = os.Rename(fullPath, fullPath+".loaded")
		if err != nil {
			return err
		}

		id += 1
		return err
	})

	l.embeddedDocs = id - 1 // Set the number of embedded documents

	l.logger.Info("rag embedding done", slog.Int("num", l.embeddedDocs))

	return err
}

func (l *Logic) Retrieve(ctx context.Context, req *ai.RetrieverRequest) (*ai.RetrieverResponse, error) {
	queryText := ""
	if req.Query != nil && len(req.Query.Content) > 0 && req.Query.Content[0].IsText() {
		queryText = req.Query.Content[0].Text
	}

	limit := 3 // default limit
	if req.Options != nil {
		if optsMap, ok := req.Options.(map[string]any); ok {
			if lVal, ok := optsMap["limit"].(float64); ok {
				limit = int(lVal)
			} else if lVal, ok := optsMap["limit"].(int); ok {
				limit = lVal
			}
		}
	}

	l.logger.Info("rag retrieve", slog.String("query", queryText), slog.Int("limit", limit))

	if l.embeddedDocs == 0 { // No embedded documents available, ignore the query
		l.logger.Warn("rag retrieve called without any embedded documents", slog.String("query", queryText), slog.Int("limit", limit))

		return &ai.RetrieverResponse{Documents: make([]*ai.Document, 0)}, nil
	}

	// Make sure we don't query more than we have embedded
	if limit > l.embeddedDocs {
		limit = l.embeddedDocs
	}

	coll := l.db.GetCollection(collectionKey, nil)
	res, err := coll.Query(ctx, queryText, limit, nil, nil)
	if err != nil {
		l.logger.Error("failed to query rag content", slog.String("query", queryText), slog.Int("limit", limit), slog.String("error", err.Error()))

		return nil, err
	}

	l.logger.Info("rag retrieve done", slog.Int("num_results", len(res)))

	docs := make([]*ai.Document, 0, len(res))
	for _, r := range res {
		doc := ai.DocumentFromText(r.Content, map[string]any{
			"similarity": r.Similarity,
			"id":         r.ID,
		})
		docs = append(docs, doc)
	}

	return &ai.RetrieverResponse{
		Documents: docs,
	}, nil
}

func (l *Logic) Evaluate(ctx context.Context, req *ai.EvaluatorRequest) (*ai.EvaluatorResponse, error) {
	var results ai.EvaluatorResponse
	for _, example := range req.Dataset {
		queryText := ""
		if inputStr, ok := example.Input.(string); ok {
			queryText = inputStr
		} else if inputDoc, ok := example.Input.(*ai.Document); ok && len(inputDoc.Content) > 0 && inputDoc.Content[0].IsText() {
			queryText = inputDoc.Content[0].Text
		}

		coll := l.db.GetCollection(collectionKey, nil)
		res, err := coll.Query(ctx, queryText, 1, nil, nil)
		if err != nil {
			return nil, err
		}

		var similarity float32 = 0
		if len(res) > 0 {
			similarity = res[0].Similarity
		}

		results = append(results, ai.EvaluationResult{
			TestCaseId: example.TestCaseId,
			Evaluation: []ai.Score{{
				Score: similarity,
			}},
		})
	}

	return &results, nil
}

func (l *Logic) Name() string {
	return "hairy-botter-rag"
}

func (l *Logic) Register(r any) {
	// Only needed if we want to dynamically register this via Genkit init, but we construct manually.
}
