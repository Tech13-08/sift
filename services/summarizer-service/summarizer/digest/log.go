package digest

import (
	"context"

	"sift/summarizer-service/summarizer/digestlog"
	"sift/summarizer-service/summarizer/model"
)

func withDigestLogUser(ctx context.Context, u model.DigestUser) context.Context {
	return digestlog.WithUser(ctx, u)
}

func digestLogf(ctx context.Context, format string, args ...any) {
	digestlog.Logf(ctx, format, args...)
}
