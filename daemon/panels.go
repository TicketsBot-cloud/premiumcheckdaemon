package daemon

import (
	"context"
	"errors"

	"github.com/TicketsBot-cloud/common/premium"
	"github.com/TicketsBot-cloud/common/sentry"
	"github.com/rxdn/gdl/cache"
)

const freePanelLimit = 3

func (d *Daemon) sweepPanels(ctx context.Context) {
	query := `SELECT "guild_id", COUNT(*) FROM panels WHERE "force_disabled" = false GROUP BY guild_id HAVING COUNT(*) > $1;`
	rows, err := d.db.Panel.Query(ctx, query, freePanelLimit)
	defer rows.Close()
	if err != nil {
		sentry.Error(err)
		return
	}

	guilds := make(map[uint64]int)
	for rows.Next() {
		var guildId uint64
		var panelCount int
		if err := rows.Scan(&guildId, &panelCount); err != nil {
			sentry.Error(err)
			continue
		}

		guilds[guildId] = panelCount
	}

	d.Logger.Printf("Detected %d guilds with > %d panels\n", len(guilds), freePanelLimit)

	var ok, notOk int

	for guildId, panelCount := range guilds {
		// get guild owner
		guild, err := d.cache.GetGuild(ctx, guildId)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				continue // if bot's been kicked doesn't matter, when we rejoin we'll purge
			} else {
				sentry.Error(err)
			}
		}

		// TODO: Ignore voting?
		tier, _, err := d.premiumClient.GetTierByGuild(ctx, guild)
		if err != nil {
			d.Logger.Printf("error getting premium status for guild %d: %s", guild.Id, err.Error())
			sentry.Error(err)
			continue
		}

		if tier < premium.Premium {
			notOk++
			d.Logger.Printf("guild %d (owner: %d) is not a patron anymore! panel count: %d (%d)\n", guildId, guild.OwnerId, panelCount, notOk)

			// Delete with select subquery destroys CPU
			// Instead, select X-3 panels first
			panels, err := d.db.Panel.GetByGuild(ctx, guildId)
			if err != nil {
				d.Logger.Printf("error getting panels for guild %d: %s", guild.Id, err.Error())
				sentry.Error(err)
				continue
			}

			// Double check
			if len(panels) < freePanelLimit {
				continue
			}

			if !d.dryRun {
				if err := d.db.Panel.DisableSome(ctx, guildId, freePanelLimit); err != nil {
					d.Logger.Printf("error disabling panels for guild %d: %s", guild.Id, err.Error())
					sentry.Error(err)
					continue
				}
			}
		} else {
			ok++
			d.Logger.Printf("guild %d (owner: %d) is ok (%d)\n", guildId, guild.OwnerId, ok)
		}
	}

	d.Logger.Printf("done panels")
}
