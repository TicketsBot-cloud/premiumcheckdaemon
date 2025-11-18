package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/TicketsBot-cloud/common/sentry"
	"github.com/TicketsBot-cloud/database"
	"github.com/TicketsBot-cloud/premiumcheckdaemon/daemon"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rxdn/gdl/cache"
)

var dryRun = flag.Bool("dryrun", false, "If set, no changes will be made")

func main() {
	flag.Parse()
	fmt.Printf("Dry-run: %v\n", *dryRun)

	if err := sentry.Initialise(sentry.Options{
		Dsn:   os.Getenv("SENTRY_DSN"),
		Debug: true,
	}); err != nil {
		fmt.Println(err.Error())
	}

	daemon := daemon.NewDaemon(newDatabaseClient(), newCacheClient(), newRedisClient(), *dryRun)
	daemon.Start()
}

func newDatabaseClient() *database.Database {
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s/%s?pool_max_conns=%s",
		os.Getenv("DATABASE_USER"),
		os.Getenv("DATABASE_PASSWORD"),
		os.Getenv("DATABASE_HOST"),
		os.Getenv("DATABASE_NAME"),
		os.Getenv("DATABASE_THREADS"),
	)

	//pool, err := pgxpool.Connect(context.Background(), connString)
	c, _ := pgxpool.ParseConfig(connString)
	c.ConnConfig.LogLevel = pgx.LogLevelTrace
	pool, err := pgxpool.ConnectConfig(context.Background(), c)
	if err != nil {
		sentry.Error(err)
		panic(err)
	}

	return database.NewDatabase(pool)
}

func newCacheClient() *cache.PgCache {
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s/%s?pool_max_conns=%s",
		os.Getenv("CACHE_USER"),
		os.Getenv("CACHE_PASSWORD"),
		os.Getenv("CACHE_HOST"),
		os.Getenv("CACHE_NAME"),
		os.Getenv("CACHE_THREADS"),
	)

	pool, err := pgxpool.Connect(context.Background(), connString)
	if err != nil {
		sentry.Error(err)
		panic(err)
	}

	client := cache.NewPgCache(pool, cache.CacheOptions{
		Guilds:      true,
		Users:       true,
		Members:     true,
		Channels:    true,
		Roles:       true,
		Emojis:      true,
		VoiceStates: true,
	})

	return &client
}

func newRedisClient() (client *redis.Client) {
	threads, err := strconv.Atoi(os.Getenv("REDIS_THREADS"))
	if err != nil {
		panic(err)
	}

	options := &redis.Options{
		Network:      "tcp",
		Addr:         os.Getenv("REDIS_ADDR"),
		Password:     os.Getenv("REDIS_PASSWD"),
		PoolSize:     threads,
		MinIdleConns: threads,
	}

	client = redis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		sentry.Error(err)
		panic(err)
	}

	return
}
