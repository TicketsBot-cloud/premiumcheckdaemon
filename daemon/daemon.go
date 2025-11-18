package daemon

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TicketsBot-cloud/common/premium"
	"github.com/TicketsBot-cloud/common/sentry"
	"github.com/TicketsBot-cloud/database"
	"github.com/TicketsBot-cloud/gdl/cache"
	"github.com/go-redis/redis/v8"
)

type Daemon struct {
	Logger        *log.Logger
	db            *database.Database
	cache         *cache.PgCache
	redis         *redis.Client
	premiumClient *premium.PremiumLookupClient
	forced        []uint64
	dryRun        bool
}

func NewDaemon(db *database.Database, cache *cache.PgCache, redis *redis.Client, dryRun bool) *Daemon {
	var forced []uint64
	for _, raw := range strings.Split(os.Getenv("FORCED"), ",") {
		if raw == "" {
			continue
		}

		userId, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			sentry.Error(err)
			continue
		}

		forced = append(forced, userId)
	}

	return &Daemon{
		Logger:        log.New(os.Stdout, "[daemon] ", log.LstdFlags),
		db:            db,
		cache:         cache,
		redis:         redis,
		premiumClient: premium.NewPremiumLookupClient(redis, cache, db),
		forced:        forced,
		dryRun:        dryRun,
	}
}

func (d *Daemon) Start() {
	for {
		select {
		case <-time.After(time.Minute * 10): // TODO: Don't hardcode
			d.Logger.Println("Starting run")
			d.doOne()
		}
	}
}

func (d *Daemon) doOne() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*10) // TODO: Don't hardcode
	defer cancel()

	d.sweepPanels(ctx)
	d.sweepWhitelabel(ctx)
}
