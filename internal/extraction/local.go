package extraction

import (
	"context"
	"fmt"

	pb "github.com/gauravfs-14/webhookmind/internal/extraction/pb"
)

// LocalExtractor wraps the gRPC client to the C++ extractor container so it satisfies the Extractor interface.
type LocalExtractor struct {
	client *ExtractionClient
}

func NewLocalExtractor(addr string, timeoutSeconds int) (*LocalExtractor, error) {
	client, err := NewExtractionClient(addr, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("new local extractor: %w", err)
	}
	return &LocalExtractor{client: client}, nil
}

func (l *LocalExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	pbResp, err := l.client.Extract(ctx, &pb.ExtractionRequest{
		EventId:      req.EventID,
		FilePath:     req.FilePath,
		FileType:     req.FileType,
		SourceId:     req.SourceID,
		PresignedUrl: req.PresignedURL,
	})
	if err != nil {
		return nil, err
	}

	segs := make([]TranscriptionSegment, 0, len(pbResp.Segments))
	for _, s := range pbResp.Segments {
		segs = append(segs, TranscriptionSegment{
			StartMs: s.StartMs,
			EndMs:   s.EndMs,
			Text:    s.Text,
		})
	}

	return &ExtractResponse{
		Success:          pbResp.Success,
		ErrorMessage:     pbResp.ErrorMessage,
		ExtractedJSON:    pbResp.ExtractedJson,
		TemplateID:       pbResp.TemplateId,
		CacheHit:         pbResp.CacheHit,
		DurationMs:       pbResp.DurationMs,
		Segments:         segs,
		DetectedLanguage: pbResp.DetectedLanguage,
	}, nil
}

func (l *LocalExtractor) Close() error {
	return l.client.Close()
}
