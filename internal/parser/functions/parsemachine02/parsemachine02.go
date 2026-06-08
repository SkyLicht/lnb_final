package parsemachine02

import (
	"context"

	"lnb_tk/internal/parser/functions/defaultparser"
	"lnb_tk/internal/parser/types"
)

func Parse(ctx context.Context, req types.Request) (types.Result, error) {
	return defaultparser.Parse(ctx, req)
}
