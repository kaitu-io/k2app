package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 访客身份解析（客服聊天）。
// 规则：强证据（cid/sid，verified）自动合并 guest，弱证据（自报邮箱，claimed）永不合并；所有合并可撤销。
// 不变量：guests.merged_into_id 要么为 NULL（根），要么直接指向一个根（深度恒为 1）。
// 设计见 docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md §4

var (
	errGuestMergeAlreadyUndone = errors.New("guest merge already undone")
	errGuestMergeStale         = errors.New("guest merge raced with another merge")
)

// guestRootMaxHops 是沿 merged_into_id 向上找根的跳数上限。
// 正常数据深度恒为 1，留余量只是为了在脏数据（环）上也能终止。
const guestRootMaxHops = 8

const (
	guestLockTTL  = 10 * time.Second
	guestLockWait = 5 * time.Second
)

// guestRootIn 沿 merged_into_id 找根，跳数有上限。
func guestRootIn(d *gorm.DB, guestID uint64) (uint64, error) {
	cur := guestID
	for i := 0; i <= guestRootMaxHops; i++ {
		var g Guest
		if err := d.Select("id", "merged_into_id").First(&g, cur).Error; err != nil {
			return 0, fmt.Errorf("load guest %d: %w", cur, err)
		}
		if g.MergedIntoID == nil {
			return g.ID, nil
		}
		cur = *g.MergedIntoID
	}
	return 0, fmt.Errorf("guest %d: merged_into chain exceeds %d hops (corrupt data)", guestID, guestRootMaxHops)
}

// guestRootID 返回 guest 所在簇的根 id。
func guestRootID(ctx context.Context, guestID uint64) (uint64, error) {
	return guestRootIn(db.Get().WithContext(ctx), guestID)
}

// guestClusterIDs 返回根 + 所有直接指向该根的 guest id。
func guestClusterIDs(ctx context.Context, rootID uint64) ([]uint64, error) {
	var ids []uint64
	if err := db.Get().WithContext(ctx).Model(&Guest{}).
		Where("id = ? OR merged_into_id = ?", rootID, rootID).Order("id").Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list guest cluster %d: %w", rootID, err)
	}
	return ids, nil
}

// lockGuestCreate 取 (brand, cid) 的短锁，串行化同一新 cid 的并发建档。
// guest_identities 的唯一键带 guest_id，挡不住两个 guest 抢同一个 cid。
func lockGuestCreate(ctx context.Context, brand Brand, cid string) (release func(), err error) {
	rdb := redis.Client()
	key := fmt.Sprintf("chat:guest:lock:%s:%s", brand, cid)
	token := generateId("glk")
	deadline := time.Now().Add(guestLockWait)
	for {
		ok, err := rdb.SetNX(ctx, key, token, guestLockTTL).Result()
		if err != nil {
			return nil, fmt.Errorf("lock guest create: %w", err)
		}
		if ok {
			return func() {
				// 仅当仍是自己的锁时才删，避免误删过期后他人拿到的锁
				const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
				_ = rdb.Eval(context.Background(), script, []string{key}, token).Err()
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("lock guest create %s: timeout", key)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// findIdentityOwner 在品牌内按值反查标识的持有 guest（取最早的一条）。
func findIdentityOwner(ctx context.Context, brand Brand, kind, value string) (*GuestIdentity, error) {
	var ident GuestIdentity
	err := db.Get().WithContext(ctx).
		Where("brand = ? AND kind = ? AND value = ?", string(brand), kind, value).
		Order("id").First(&ident).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find %s identity: %w", kind, err)
	}
	return &ident, nil
}

// touchGuest 刷新 last_seen_at；locale/country 非空才覆盖。
func touchGuest(ctx context.Context, guestID uint64, locale, country string) error {
	upd := map[string]any{"last_seen_at": time.Now()}
	if locale != "" {
		upd["locale"] = locale
	}
	if country != "" {
		upd["country"] = country
	}
	if err := db.Get().WithContext(ctx).Model(&Guest{}).Where("id = ?", guestID).Updates(upd).Error; err != nil {
		return fmt.Errorf("touch guest %d: %w", guestID, err)
	}
	return nil
}

// upsertIdentity 幂等挂标识：已存在只刷新 last_seen_at（不改 strength，不降级）。
func upsertIdentity(ctx context.Context, guestID uint64, brand Brand, kind, value, strength string) (uint64, error) {
	now := time.Now()
	ident := GuestIdentity{GuestID: guestID, Brand: string(brand), Kind: kind, Value: value,
		Strength: strength, FirstSeenAt: now, LastSeenAt: now}
	d := db.Get().WithContext(ctx)
	if err := d.Clauses(clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"last_seen_at"})}).
		Create(&ident).Error; err != nil {
		return 0, fmt.Errorf("upsert %s identity: %w", kind, err)
	}
	var id uint64
	if err := d.Model(&GuestIdentity{}).Where("guest_id = ? AND kind = ? AND value = ?", guestID, kind, value).
		Pluck("id", &id).Error; err != nil {
		return 0, fmt.Errorf("reload %s identity: %w", kind, err)
	}
	return id, nil
}

// createGuestWithCID 事务内建 guest 并挂 cid。
func createGuestWithCID(ctx context.Context, brand Brand, cid, locale, country string) (uint64, error) {
	now := time.Now()
	g := Guest{Brand: string(brand), Locale: locale, Country: country, FirstSeenAt: now, LastSeenAt: now}
	err := db.Get().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		return tx.Create(&GuestIdentity{GuestID: g.ID, Brand: string(brand), Kind: IdentityCID, Value: cid,
			Strength: StrengthVerified, FirstSeenAt: now, LastSeenAt: now}).Error
	})
	if err != nil {
		return 0, fmt.Errorf("create guest: %w", err)
	}
	return g.ID, nil
}

// resolveGuest 返回访客所在簇的根 guest id。
// cid 不存在则新建 guest 并挂 cid；sid 非空则挂到根上，若该 sid 已挂在同品牌另一个根上则自动合并（same_sid）。
func resolveGuest(ctx context.Context, brand Brand, cid, sid, locale, country string) (uint64, error) {
	if cid == "" {
		return 0, errors.New("resolveGuest: empty cid")
	}

	var guestID uint64
	owner, err := findIdentityOwner(ctx, brand, IdentityCID, cid)
	if err != nil {
		return 0, err
	}
	if owner == nil {
		release, err := lockGuestCreate(ctx, brand, cid)
		if err != nil {
			return 0, err
		}
		func() {
			defer release()
			// 拿到锁后复查：别的请求可能刚建好
			if owner, err = findIdentityOwner(ctx, brand, IdentityCID, cid); err != nil || owner != nil {
				return
			}
			guestID, err = createGuestWithCID(ctx, brand, cid, locale, country)
		}()
		if err != nil {
			return 0, err
		}
	}
	if owner != nil {
		guestID = owner.GuestID
		if err := db.Get().WithContext(ctx).Model(&GuestIdentity{}).Where("id = ?", owner.ID).
			Update("last_seen_at", time.Now()).Error; err != nil {
			return 0, fmt.Errorf("touch cid identity: %w", err)
		}
		if err := touchGuest(ctx, guestID, locale, country); err != nil {
			return 0, err
		}
	}

	root, err := guestRootID(ctx, guestID)
	if err != nil {
		return 0, err
	}
	if sid == "" {
		return root, nil
	}

	if _, err := upsertIdentity(ctx, root, brand, IdentitySID, sid, StrengthVerified); err != nil {
		return 0, err
	}
	var holders []GuestIdentity
	if err := db.Get().WithContext(ctx).
		Where("brand = ? AND kind = ? AND value = ? AND guest_id <> ?", string(brand), IdentitySID, sid, root).
		Order("id").Find(&holders).Error; err != nil {
		return 0, fmt.Errorf("find sid holders: %w", err)
	}
	for _, h := range holders {
		other, err := guestRootID(ctx, h.GuestID)
		if err != nil {
			return 0, err
		}
		hid := h.ID
		if _, err := mergeGuests(ctx, other, root, MergeSameSID, &hid, nil); err != nil {
			return 0, err
		}
	}
	return guestRootID(ctx, root)
}

// mergeGuests 把 from 的整簇并入 into 的根（方向以较早 first_seen_at 者存活，与入参顺序无关），
// 并压平：所有指向被并入根的行改指存活根。同根返回 nil, nil。
func mergeGuests(ctx context.Context, fromID, intoID uint64, reason string, evidenceIdentityID, actorID *uint64) (*GuestMerge, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		m, err := mergeGuestsOnce(ctx, fromID, intoID, reason, evidenceIdentityID, actorID)
		if !errors.Is(err, errGuestMergeStale) {
			return m, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func mergeGuestsOnce(ctx context.Context, fromID, intoID uint64, reason string, evidenceIdentityID, actorID *uint64) (*GuestMerge, error) {
	var out *GuestMerge
	err := db.Get().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, err := guestRootIn(tx, fromID)
		if err != nil {
			return err
		}
		b, err := guestRootIn(tx, intoID)
		if err != nil {
			return err
		}
		if a == b {
			return nil
		}
		// 按 id 顺序锁两个根，再复查它们仍是根（期间可能被别的合并并走）
		var roots []Guest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", []uint64{a, b}).Order("id").Find(&roots).Error; err != nil {
			return fmt.Errorf("lock guest roots: %w", err)
		}
		if len(roots) != 2 {
			return fmt.Errorf("merge guests: root rows missing (%d,%d)", a, b)
		}
		for _, r := range roots {
			if r.MergedIntoID != nil {
				return errGuestMergeStale
			}
		}
		// 较早创建者存活；同时刻按 id 小者存活
		survivor, loser := roots[0], roots[1]
		if roots[1].FirstSeenAt.Before(roots[0].FirstSeenAt) {
			survivor, loser = roots[1], roots[0]
		}

		var moved []uint64
		if err := tx.Model(&Guest{}).Where("id = ? OR merged_into_id = ?", loser.ID, loser.ID).
			Order("id").Pluck("id", &moved).Error; err != nil {
			return fmt.Errorf("list moved guests: %w", err)
		}
		if err := tx.Model(&Guest{}).Where("id IN ?", moved).
			Update("merged_into_id", survivor.ID).Error; err != nil {
			return fmt.Errorf("repoint guests: %w", err)
		}
		movedJSON, err := json.Marshal(moved)
		if err != nil {
			return fmt.Errorf("marshal moved ids: %w", err)
		}
		rec := &GuestMerge{FromGuestID: loser.ID, IntoGuestID: survivor.ID, Reason: reason,
			EvidenceIdentityID: evidenceIdentityID, ActorID: actorID,
			MovedGuestIDs: string(movedJSON), CreatedAt: time.Now()}
		if err := tx.Create(rec).Error; err != nil {
			return fmt.Errorf("record guest merge: %w", err)
		}
		out = rec
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// undoGuestMerge 撤销一次合并：只还原 MovedGuestIDs 里的行——
// 原 from 根重新成为根，其余被一并移动的行改回指向它。
func undoGuestMerge(ctx context.Context, mergeID uint64, actorID uint64) error {
	return db.Get().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec GuestMerge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rec, mergeID).Error; err != nil {
			return fmt.Errorf("load guest merge %d: %w", mergeID, err)
		}
		if rec.UndoneAt != nil {
			return errGuestMergeAlreadyUndone
		}
		var moved []uint64
		if err := json.Unmarshal([]byte(rec.MovedGuestIDs), &moved); err != nil {
			return fmt.Errorf("parse moved ids of merge %d: %w", mergeID, err)
		}
		var rest []uint64
		for _, id := range moved {
			if id != rec.FromGuestID {
				rest = append(rest, id)
			}
		}
		if err := tx.Model(&Guest{}).Where("id = ?", rec.FromGuestID).
			Update("merged_into_id", nil).Error; err != nil {
			return fmt.Errorf("restore from root: %w", err)
		}
		if len(rest) > 0 {
			if err := tx.Model(&Guest{}).Where("id IN ?", rest).
				Update("merged_into_id", rec.FromGuestID).Error; err != nil {
				return fmt.Errorf("restore moved guests: %w", err)
			}
		}
		now := time.Now()
		if err := tx.Model(&GuestMerge{}).Where("id = ?", rec.ID).
			Updates(map[string]any{"undone_at": now, "undone_by": actorID}).Error; err != nil {
			return fmt.Errorf("mark merge undone: %w", err)
		}
		return nil
	})
}

// addGuestEmail 给 guest 挂自报邮箱（strength=claimed，幂等）。弱证据：不触发任何合并。
func addGuestEmail(ctx context.Context, rootID uint64, brand Brand, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("addGuestEmail: empty email")
	}
	_, err := upsertIdentity(ctx, rootID, brand, IdentityEmail, email, StrengthClaimed)
	return err
}

// guestEmail 返回簇内最近一条 email 标识，无则 ""。
func guestEmail(ctx context.Context, rootID uint64) string {
	ids, err := guestClusterIDs(ctx, rootID)
	if err != nil || len(ids) == 0 {
		return ""
	}
	var ident GuestIdentity
	if err := db.Get().WithContext(ctx).Where("guest_id IN ? AND kind = ?", ids, IdentityEmail).
		Order("last_seen_at DESC, id DESC").First(&ident).Error; err != nil {
		return ""
	}
	return ident.Value
}
