package daemon

import (
	"context"
	"errors"

	"github.com/TicketsBot-cloud/common/premium"
	"github.com/TicketsBot-cloud/common/sentry"
	gdlcache "github.com/TicketsBot-cloud/gdl/cache"
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
				if err := d.db.Panel.ForceDisableSome(ctx, guildId, freePanelLimit); err != nil {
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

// sweepComponentsV2 downgrades Components V2 usage for guilds that are no longer Premium.
// It has three independent responsibilities, each a straight conditional UPDATE (there is no
// quota to balance here, unlike sweepPanels, just "is this guild still allowed to use this"):
//
//  1. Panels with a Components V2 button message are force-disabled, the same way panels over
//     the free quota are force-disabled in sweepPanels.
//  2. Panels with a Components V2 welcome message have that setting cleared (not force-disabled).
//     This intentionally leaves the panel's button message and force_disabled state untouched;
//     the welcome message falls back to the classic hardcoded text once cleared.
//  3. Multi-panels using Components V2 are force-disabled.
func (d *Daemon) sweepComponentsV2(ctx context.Context) {
	d.sweepComponentsV2ButtonMessages(ctx)
	d.sweepComponentsV2WelcomeMessages(ctx)
	d.sweepComponentsV2MultiPanels(ctx)

	d.Logger.Printf("done components v2")
}

// guildBelowPremium reports whether a guild's current premium tier is below Premium. If the
// guild can't be found in the cache (e.g. the bot has been kicked), it is treated as not
// below Premium, since there is nothing to sweep.
func (d *Daemon) guildBelowPremium(ctx context.Context, guildId uint64) (bool, error) {
	guild, err := d.cache.GetGuild(ctx, guildId)
	if err != nil {
		if errors.Is(err, gdlcache.ErrNotFound) {
			return false, nil
		}

		return false, err
	}

	tier, _, err := d.premiumClient.GetTierByGuild(ctx, guild)
	if err != nil {
		return false, err
	}

	return tier < premium.Premium, nil
}

func (d *Daemon) sweepComponentsV2ButtonMessages(ctx context.Context) {
	query := `SELECT DISTINCT "guild_id" FROM panels WHERE "message_uses_components_v2" = true AND "force_disabled" = false;`
	rows, err := d.db.Panel.Query(ctx, query)
	if err != nil {
		sentry.Error(err)
		return
	}
	defer rows.Close()

	var guildIds []uint64
	for rows.Next() {
		var guildId uint64
		if err := rows.Scan(&guildId); err != nil {
			sentry.Error(err)
			continue
		}

		guildIds = append(guildIds, guildId)
	}

	for _, guildId := range guildIds {
		belowPremium, err := d.guildBelowPremium(ctx, guildId)
		if err != nil {
			d.Logger.Printf("error getting premium status for guild %d: %s", guildId, err.Error())
			sentry.Error(err)
			continue
		}

		if !belowPremium {
			continue
		}

		d.Logger.Printf("guild %d is no longer premium, force disabling components v2 panel messages\n", guildId)

		if !d.dryRun {
			if err := d.db.Panel.SetComponentsV2ForceDisabled(ctx, guildId, true); err != nil {
				d.Logger.Printf("error force disabling components v2 panels for guild %d: %s", guildId, err.Error())
				sentry.Error(err)
			}
		}
	}
}

func (d *Daemon) sweepComponentsV2WelcomeMessages(ctx context.Context) {
	query := `SELECT DISTINCT "guild_id" FROM panels WHERE "welcome_message_uses_components_v2" = true;`
	rows, err := d.db.Panel.Query(ctx, query)
	if err != nil {
		sentry.Error(err)
		return
	}
	defer rows.Close()

	var guildIds []uint64
	for rows.Next() {
		var guildId uint64
		if err := rows.Scan(&guildId); err != nil {
			sentry.Error(err)
			continue
		}

		guildIds = append(guildIds, guildId)
	}

	for _, guildId := range guildIds {
		belowPremium, err := d.guildBelowPremium(ctx, guildId)
		if err != nil {
			d.Logger.Printf("error getting premium status for guild %d: %s", guildId, err.Error())
			sentry.Error(err)
			continue
		}

		if !belowPremium {
			continue
		}

		d.Logger.Printf("guild %d is no longer premium, clearing components v2 welcome message\n", guildId)

		if !d.dryRun {
			if err := d.db.Panel.ClearWelcomeMessageComponentsV2(ctx, guildId); err != nil {
				d.Logger.Printf("error clearing components v2 welcome message for guild %d: %s", guildId, err.Error())
				sentry.Error(err)
			}
		}
	}
}

func (d *Daemon) sweepComponentsV2MultiPanels(ctx context.Context) {
	query := `SELECT DISTINCT "guild_id" FROM multi_panels WHERE "uses_components_v2" = true AND "force_disabled" = false;`
	rows, err := d.db.MultiPanels.Query(ctx, query)
	if err != nil {
		sentry.Error(err)
		return
	}
	defer rows.Close()

	var guildIds []uint64
	for rows.Next() {
		var guildId uint64
		if err := rows.Scan(&guildId); err != nil {
			sentry.Error(err)
			continue
		}

		guildIds = append(guildIds, guildId)
	}

	for _, guildId := range guildIds {
		belowPremium, err := d.guildBelowPremium(ctx, guildId)
		if err != nil {
			d.Logger.Printf("error getting premium status for guild %d: %s", guildId, err.Error())
			sentry.Error(err)
			continue
		}

		if !belowPremium {
			continue
		}

		d.Logger.Printf("guild %d is no longer premium, force disabling components v2 multi-panels\n", guildId)

		if !d.dryRun {
			if err := d.db.MultiPanels.SetForceDisabled(ctx, guildId, true); err != nil {
				d.Logger.Printf("error force disabling components v2 multi-panels for guild %d: %s", guildId, err.Error())
				sentry.Error(err)
			}
		}
	}
}
