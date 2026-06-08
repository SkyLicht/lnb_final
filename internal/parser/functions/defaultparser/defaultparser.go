package defaultparser

import (
	"context"
	"strings"

	"lnb_tk/internal/parser/types"
)

func Parse(ctx context.Context, req types.Request) (types.Result, error) {
	return lineParser(ctx, req.Content)
}

func lineParser(ctx context.Context, content []byte) (types.Result, error) {
	select {
	case <-ctx.Done():
		return types.Result{}, ctx.Err()
	default:
	}

	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return types.Result{Records: 0}, nil
	}
	return types.Result{Records: len(strings.Split(trimmed, "\n"))}, nil
}
