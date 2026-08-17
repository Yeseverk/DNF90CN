package pipeline

import "context"

type Handler[T any, R any] func(context.Context, T) (R, error)

type Before[T any] func(context.Context, T) (context.Context, T, error)

type After[R any] func(context.Context, R, error) (R, error)

type Pipeline[T any, R any] struct {
	before []Before[T]
	after  []After[R]
}

func New[T any, R any]() *Pipeline[T, R] {
	return &Pipeline[T, R]{}
}

func (p *Pipeline[T, R]) Clone() *Pipeline[T, R] {
	if p == nil {
		return New[T, R]()
	}
	return &Pipeline[T, R]{
		before: append([]Before[T](nil), p.before...),
		after:  append([]After[R](nil), p.after...),
	}
}

func (p *Pipeline[T, R]) UseBefore(fn Before[T]) {
	if fn != nil {
		p.before = append(p.before, fn)
	}
}

func (p *Pipeline[T, R]) UseAfter(fn After[R]) {
	if fn != nil {
		p.after = append(p.after, fn)
	}
}

func (p *Pipeline[T, R]) Execute(ctx context.Context, req T, handler Handler[T, R]) (R, error) {
	var zero R
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil {
		if handler == nil {
			return zero, nil
		}
		return handler(ctx, req)
	}
	for _, before := range p.before {
		var err error
		ctx, req, err = before(ctx, req)
		if err != nil {
			return zero, err
		}
	}
	if handler == nil {
		return zero, nil
	}
	resp, err := handler(ctx, req)
	for _, after := range p.after {
		resp, err = after(ctx, resp, err)
	}
	return resp, err
}
