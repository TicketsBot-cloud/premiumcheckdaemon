package daemon

import (
	"context"

	"github.com/TicketsBot-cloud/common/model"
	"github.com/TicketsBot-cloud/common/premium"
	"github.com/TicketsBot-cloud/common/sentry"
	"github.com/TicketsBot-cloud/common/whitelabeldelete"
)

func (d *Daemon) sweepWhitelabel(ctx context.Context) {
	query := `SELECT "user_id" FROM whitelabel;`
	rows, err := d.db.Whitelabel.Query(ctx, query)
	defer rows.Close()

	if err != nil {
		sentry.Error(err)
		d.Logger.Printf("error getting whitelabel users: %s", err.Error())
		return
	}

	for rows.Next() {
		var userId uint64
		if err := rows.Scan(&userId); err != nil {
			sentry.Error(err)
			continue
		}

		entitlements, err := d.db.Entitlements.ListUserSubscriptions(ctx, userId, premium.GracePeriod)
		if err != nil {
			sentry.Error(err)
			d.Logger.Printf("error getting entitlements for %d: %s", userId, err.Error())
			return
		}

		hasWhitelabel := false
		for _, entitlement := range entitlements {
			if entitlement.Tier == model.EntitlementTierWhitelabel {
				hasWhitelabel = true
				break
			}
		}

		if !hasWhitelabel {
			// get bot ID
			bot, err := d.db.Whitelabel.GetByUserId(ctx, userId)
			if err != nil {
				sentry.Error(err)
				return
			}

			d.Logger.Printf("whitelabel: deleting bot %d (user %d)\n", bot.BotId, bot.UserId)

			if !d.dryRun {
				if _, err := d.db.Whitelabel.Delete(ctx, userId); err != nil {
					sentry.Error(err)
					d.Logger.Printf("error deleting whitelabel for %d: %s", userId, err.Error())
					return
				}

				whitelabeldelete.Publish(d.redis, bot.BotId)
			}
		}
	}

	d.Logger.Println("Done whitelabel")
}
