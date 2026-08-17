package bus

import (
	"context"

	"longheng.io/server/internal/platform/tracing"
)

func envelopeWithTrace(ctx context.Context, env Envelope) Envelope {
	env.Trace = tracing.InjectCarrier(ctx, env.Trace)
	return env
}

func contextWithTrace(ctx context.Context, env Envelope) context.Context {
	return tracing.ExtractCarrier(ctx, env.Trace)
}
