package repository

import "context"

func (b *Base[R]) InsertMany(ctx context.Context, columns []string, rows [][]any) error {
	return b.DAO(ctx).InsertMany(ctx, columns, rows)
}

func (b *Base[R]) CopyFrom(ctx context.Context, columns []string, rows [][]any) (int64, error) {
	return b.DAO(ctx).CopyFrom(ctx, columns, rows)
}

func (b *Base[R]) UpdateByIDs(ctx context.Context, ids []string, set map[string]any) error {
	return b.DAO(ctx).UpdateByIDs(ctx, ids, set)
}

func (b *Base[R]) DeleteByIDs(ctx context.Context, ids []string) error {
	return b.DAO(ctx).DeleteByIDs(ctx, ids)
}

func (b *Base[R]) FindByIDs(ctx context.Context, ids []string) ([]R, error) {
	return b.DAO(ctx).FindByIDs(ctx, ids)
}
