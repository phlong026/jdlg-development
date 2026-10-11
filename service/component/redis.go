package component

import (
	"context"
	"log"
	"github.com/redis/go-redis/v9"
	"service/config"
)

var RedisClient *redis.Client

// InitRedis 初始化Redis客户端
func InitRedis(cfg *config.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// 测试连接
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	RedisClient = rdb
	log.Println("Redis 连接成功")
	return rdb, nil
}

// GetRedis 获取Redis客户端
func GetRedis() *redis.Client {
	return RedisClient
}
