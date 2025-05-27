package geminiembedding

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/genai"
	"google.golang.org/genai/generativelanguage/apiv1beta/generativepb"
)

// mockAIClient is a mock implementation of the aiClient interface.
type mockAIClient struct {
	EmbedContentFunc func(ctx context.Context, model string, parts ...genai.Part) (*genai.EmbedContentResponse, error)
}

func (m *mockAIClient) EmbedContent(ctx context.Context, model string, parts ...genai.Part) (*genai.EmbedContentResponse, error) {
	if m.EmbedContentFunc != nil {
		return m.EmbedContentFunc(ctx, model, parts...)
	}
	return nil, errors.New("EmbedContentFunc not implemented")
}

func TestGeminiEmbeddingFunc(t *testing.T) {
	// Test Case 1: Successful embedding
	t.Run("SuccessfulEmbedding", func(t *testing.T) {
		mockClient := &mockAIClient{
			EmbedContentFunc: func(ctx context.Context, model string, parts ...genai.Part) (*genai.EmbedContentResponse, error) {
				return &genai.EmbedContentResponse{
					Embedding: &generativepb.Embedding{
						Value: []float32{0.1, 0.2, 0.3},
					},
				}, nil
			},
		}

		embeddingFunc := GeminiEmbeddingFunc(mockClient, "test-model")
		embedding, err := embeddingFunc(context.Background(), "hello world")

		if err != nil {
			t.Errorf("Expected no error, but got %v", err)
		}

		expectedEmbedding := []float32{0.1, 0.2, 0.3}
		if !reflect.DeepEqual(embedding, expectedEmbedding) {
			t.Errorf("Expected embedding %v, but got %v", expectedEmbedding, embedding)
		}
	})

	// Test Case 2: Error from EmbedContent
	t.Run("ErrorFromEmbedContent", func(t *testing.T) {
		mockErr := errors.New("mock EmbedContent error")
		mockClient := &mockAIClient{
			EmbedContentFunc: func(ctx context.Context, model string, parts ...genai.Part) (*genai.EmbedContentResponse, error) {
				return nil, mockErr
			},
		}

		embeddingFunc := GeminiEmbeddingFunc(mockClient, "test-model")
		embedding, err := embeddingFunc(context.Background(), "hello world")

		if err == nil {
			t.Errorf("Expected an error, but got nil")
		}

		if !errors.Is(err, mockErr) {
			t.Errorf("Expected error %v, but got %v", mockErr, err)
		}

		if embedding != nil {
			t.Errorf("Expected nil embedding, but got %v", embedding)
		}
	})

	// Test Case 3: No embeddings returned
	t.Run("NoEmbeddingsReturned", func(t *testing.T) {
		mockClient := &mockAIClient{
			EmbedContentFunc: func(ctx context.Context, model string, parts ...genai.Part) (*genai.EmbedContentResponse, error) {
				return &genai.EmbedContentResponse{
					Embedding: &generativepb.Embedding{
						Value: []float32{},
					},
				}, nil
			},
		}

		embeddingFunc := GeminiEmbeddingFunc(mockClient, "test-model")
		embedding, err := embeddingFunc(context.Background(), "hello world")

		if err == nil {
			t.Errorf("Expected an error, but got nil")
		}
		
		// Ideally, check for the specific error message "no embeddings returned"
		// For now, just check if an error is returned.
		// The actual error message depends on the implementation of GeminiEmbeddingFunc, 
		// which is not provided in this task.

		if embedding != nil {
			t.Errorf("Expected nil embedding, but got %v", embedding)
		}
	})
}
