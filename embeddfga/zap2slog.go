package embeddfga

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/openfga/openfga/pkg/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type zap2Slog struct {
	slog slog.Handler
}

func (s2 zap2Slog) Debug(s string, field ...zap.Field) {
	s2.log(context.Background(), slog.LevelDebug, s, field...)
}

func (s2 zap2Slog) Info(s string, field ...zap.Field) {
	s2.log(context.Background(), slog.LevelInfo, s, field...)
}

func (s2 zap2Slog) Warn(s string, field ...zap.Field) {
	s2.log(context.Background(), slog.LevelWarn, s, field...)
}

func (s2 zap2Slog) Error(s string, field ...zap.Field) {
	s2.log(context.Background(), slog.LevelError, s, field...)
}

func (s2 zap2Slog) Panic(s string, field ...zap.Field) {
	s2.log(context.Background(), slog.LevelError, s, field...)
	panic(s)
}

func (s2 zap2Slog) Fatal(s string, field ...zap.Field) {
	s2.log(context.Background(), slog.LevelError, s, field...)
	os.Exit(1)
}

func (s2 zap2Slog) With(field ...zap.Field) logger.Logger {
	attrs := zapFieldsToAttrs(field)
	cloned := zap2Slog{
		slog: s2.slog.WithAttrs(attrs),
	}
	return &cloned
}

func (s2 zap2Slog) DebugWithContext(ctx context.Context, s string, field ...zap.Field) {
	s2.log(ctx, slog.LevelDebug, s, field...)
}

func (s2 zap2Slog) InfoWithContext(ctx context.Context, s string, field ...zap.Field) {
	s2.log(ctx, slog.LevelInfo, s, field...)
}

func (s2 zap2Slog) WarnWithContext(ctx context.Context, s string, field ...zap.Field) {
	s2.log(ctx, slog.LevelWarn, s, field...)
}

func (s2 zap2Slog) ErrorWithContext(ctx context.Context, s string, field ...zap.Field) {
	s2.log(ctx, slog.LevelError, s, field...)
}

func (s2 zap2Slog) PanicWithContext(ctx context.Context, s string, field ...zap.Field) {
	s2.log(ctx, slog.LevelError, s, field...)
	panic(s)
}

func (s2 zap2Slog) FatalWithContext(ctx context.Context, s string, field ...zap.Field) {
	s2.log(ctx, slog.LevelError, s, field...)
	os.Exit(1)
}

var _ logger.Logger = (*zap2Slog)(nil)

// log is a helper that builds a slog.Record and sends it to the handler.
func (s2 zap2Slog) log(ctx context.Context, level slog.Level, msg string, fields ...zap.Field) {
	if s2.slog == nil {
		return
	}
	if !s2.slog.Enabled(ctx, level) {
		return
	}
	rec := slog.Record{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
	}
	for _, attr := range zapFieldsToAttrs(fields) {
		rec.AddAttrs(attr)
	}
	_ = s2.slog.Handle(ctx, rec)
}

// zapFieldsToAttrs converts zap fields into slog attributes using MapObjectEncoder.
func zapFieldsToAttrs(fields []zap.Field) []slog.Attr {
	if len(fields) == 0 {
		return nil
	}
	enc := zapcore.NewMapObjectEncoder()
	for i := range fields {
		fields[i].AddTo(enc)
	}
	attrs := make([]slog.Attr, 0, len(enc.Fields))
	for k, v := range enc.Fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	return attrs
}
