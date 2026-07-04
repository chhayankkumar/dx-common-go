package repository

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/postgres/dao"
	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

func (b *Base[R]) FindPage(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy, limit, offset int) (*dao.Page[R], error) {
	return b.DAO(ctx).FindPage(ctx, conditions, orderBy, limit, offset)
}

func (b *Base[R]) Count(ctx context.Context, conditions []query.Condition) (int64, error) {
	return b.DAO(ctx).Count(ctx, conditions)
}

func (b *Base[R]) Exists(ctx context.Context, conditions []query.Condition) (bool, error) {
	return b.DAO(ctx).Exists(ctx, conditions)
}
