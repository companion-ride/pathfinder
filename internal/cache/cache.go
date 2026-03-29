package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultTTL = 3 * time.Hour

var ErrCacheMiss = errors.New("cache miss")

type Cache struct {
	client *redis.Client
}

func New(addr string) *Cache {
	return &Cache{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: os.Getenv("REDIS_PASSWORD"),
		}),
	}
}

func routeKey(routeID string) string    { return fmt.Sprintf("route:%s", routeID) }
func closestKey(routeID string) string  { return fmt.Sprintf("closest:%s", routeID) }
func walkKey(routeID string) string     { return fmt.Sprintf("walk:%s", routeID) }
func rawRouteKey(routeID string) string { return fmt.Sprintf("raw_route:%s", routeID) }

// SetRoute caches the full route response as JSON.
func (c *Cache) SetRoute(ctx context.Context, routeID string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, routeKey(routeID), b, defaultTTL).Err()
}

// GetRoute returns the raw JSON string for a cached route, or ErrCacheMiss.
func (c *Cache) GetRoute(ctx context.Context, routeID string) (string, error) {
	val, err := c.client.Get(ctx, routeKey(routeID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// AddClosestPoint appends a closest-point entry to the route's list and refreshes TTL.
func (c *Cache) AddClosestPoint(ctx context.Context, routeID string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	key := closestKey(routeID)
	pipe := c.client.Pipeline()
	pipe.RPush(ctx, key, b)
	pipe.LTrim(ctx, key, -100, -1)
	pipe.Expire(ctx, key, defaultTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// GetClosestPoints returns all closest-point entries as raw JSON strings, or ErrCacheMiss.
func (c *Cache) GetClosestPoints(ctx context.Context, routeID string) ([]string, error) {
	key := closestKey(routeID)
	vals, err := c.client.LRange(ctx, key, 0, -1).Result()
	if errors.Is(err, redis.Nil) || (err == nil && len(vals) == 0) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return vals, nil
}

// AddWalk appends a walk entry to the route's list and refreshes TTL.
func (c *Cache) AddWalk(ctx context.Context, routeID string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	key := walkKey(routeID)
	pipe := c.client.Pipeline()
	pipe.RPush(ctx, key, b)
	pipe.LTrim(ctx, key, -100, -1)
	pipe.Expire(ctx, key, defaultTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// GetWalks returns all walk entries as raw JSON strings, or ErrCacheMiss.
func (c *Cache) GetWalks(ctx context.Context, routeID string) ([]string, error) {
	key := walkKey(routeID)
	vals, err := c.client.LRange(ctx, key, 0, -1).Result()
	if errors.Is(err, redis.Nil) || (err == nil && len(vals) == 0) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return vals, nil
}

// SetRawResponse caches the raw 2GIS API response bytes.
func (c *Cache) SetRawResponse(ctx context.Context, routeID string, raw []byte) error {
	return c.client.Set(ctx, rawRouteKey(routeID), raw, defaultTTL).Err()
}

// GetRawResponse returns the raw 2GIS API response, or ErrCacheMiss.
func (c *Cache) GetRawResponse(ctx context.Context, routeID string) (string, error) {
	val, err := c.client.Get(ctx, rawRouteKey(routeID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	if err != nil {
		return "", err
	}
	return val, nil
}
