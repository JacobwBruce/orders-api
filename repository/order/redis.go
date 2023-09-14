package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jacobwbruce/orders-api/model"
	"github.com/redis/go-redis/v9"
)

type RedisRepository struct {
	Client *redis.Client
}

func orderIDKey(orderID uint64) string {
	return fmt.Sprintf("order:%d", orderID)
}

func (r *RedisRepository) Insert(ctx context.Context, order model.Order) error {
	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to encode order: %w", err)
	}

	key := orderIDKey(order.OrderID)

	transaction := r.Client.TxPipeline()

	err = transaction.SetNX(ctx, key, data, 0).Err()
	if err != nil {
		transaction.Discard()
		return fmt.Errorf("failed to insert order: %w", err)
	}

	if err := transaction.SAdd(ctx, "orders", key).Err(); err != nil {
		transaction.Discard()
		return fmt.Errorf("failed to add orders set: %w", err)
	}

	if _, err = transaction.Exec(ctx); err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}

	return nil
}

var ErrNotExist = errors.New("order does not exist")

func (r *RedisRepository) FindByID(ctx context.Context, id uint64) (model.Order, error) {
	key := orderIDKey(id)

	value, err := r.Client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return model.Order{}, ErrNotExist
	} else if err != nil {
		return model.Order{}, fmt.Errorf("failed to get order: %w", err)
	}

	var order model.Order
	err = json.Unmarshal([]byte(value), &order)
	if err != nil {
		return model.Order{}, fmt.Errorf("failed to decode order: %w", err)
	}

	return order, nil
}

func (r *RedisRepository) DeleteByID(ctx context.Context, id uint64) error {
	key := orderIDKey(id)

	transaction := r.Client.TxPipeline()

	err := transaction.Del(ctx, key).Err()
	if errors.Is(err, redis.Nil) {
		transaction.Discard()
		return ErrNotExist
	} else if err != nil {
		transaction.Discard()
		return fmt.Errorf("failed to get order: %w", err)
	}

	if err := transaction.SRem(ctx, "orders", key).Err(); err != nil {
		return fmt.Errorf("failed to remove orders set: %w", err)
	}

	return nil
}

func (r *RedisRepository) Update(ctx context.Context, order model.Order) error {
	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to encode order: %w", err)
	}

	key := orderIDKey(order.OrderID)

	err = r.Client.SetXX(ctx, key, string(data), 0).Err()
	if errors.Is(err, redis.Nil) {
		return ErrNotExist
	} else if err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	return nil

}

type FindAllPage struct {
	Size   uint64
	Offset uint64
}

type FindResult struct {
	Orders []model.Order
	Cursor uint64
}

func (r *RedisRepository) FindAll(ctx context.Context, page FindAllPage) (FindResult, error) {
	res := r.Client.SScan(ctx, "orders", page.Offset, "*", int64(page.Size))

	keys, cursor, err := res.Result()
	if err != nil {
		return FindResult{}, fmt.Errorf("failed to get order IDs: %w", err)
	}

	if len(keys) == 0 {
		return FindResult{
			Orders: []model.Order{},
		}, nil
	}

	xs, err := r.Client.MGet(ctx, keys...).Result()
	if err != nil {
		return FindResult{}, fmt.Errorf("failed to get orders: %w", err)
	}

	orders := make([]model.Order, len(xs))

	for i, x := range xs {
		x := x.(string)
		var order model.Order

		err := json.Unmarshal([]byte(x), &order)
		if err != nil {
			return FindResult{}, fmt.Errorf("failed to decode json: %w", err)
		}

		orders[i] = order
	}

	return FindResult{
		Orders: orders,
		Cursor: cursor,
	}, nil

}
