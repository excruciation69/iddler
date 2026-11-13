package steam

// Achievements: read the stats schema and flip achievement bits (what Steam Achievement Manager does).

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type Achievement struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Desc       string  `json:"desc"`
	Icon       string  `json:"icon"`
	Unlocked   bool    `json:"unlocked"`
	UnlockedAt int64   `json:"unlockedAt"`
	Protected  bool    `json:"protected"`
	Hidden     bool    `json:"hidden"`
	Percent    float64 `json:"percent"`
	block      uint32
	bit        uint
}

type GameStats struct {
	AppID        uint32
	crc          uint32
	blocks       map[uint32]uint32 // achievement block id -> bitmask
	Achievements []Achievement
}

func (c *Client) UserStats(appID uint32, steamID uint64, lang string) (*GameStats, error) {
	req := NewPB().Fixed64(1, uint64(appID)).Uint(2, 0).Int(3, -1).Fixed64(4, steamID)
	p, err := c.RequestReply(EMsgClientGetUserStats, req, EMsgClientGetUserStatsResponse, 20*time.Second)
	if err != nil {
		return nil, err
	}
	m := p.Fields()
	if res := int(m.I(2)); res != ResOK {
		return nil, fmt.Errorf("%s", ResultName(res))
	}
	return parseStats(appID, m, lang)
}

func parseStats(appID uint32, m Fields, lang string) (*GameStats, error) {
	gs := &GameStats{AppID: appID, crc: uint32(m.U(3)), blocks: map[uint32]uint32{}}
	for _, s := range m.Msgs(5) {
		gs.blocks[uint32(s.U(1))] = uint32(s.U(2))
	}
	unlockTimes := map[uint32][]uint64{}
	for _, b := range m.Msgs(6) {
		unlockTimes[uint32(b.U(1))] = b.Uints(2)
	}
	kv, err := ParseKV(m.Raw(4))
	if err != nil || len(kv.Children) == 0 {
		return nil, errors.New("this game has no achievements schema")
	}
	stats := kv.Children[0].Get("stats")
	for _, blk := range stats.Children {
		t := blk.Get("type_int").Str()
		if t == "" {
			t = blk.Get("type").Str()
		}
		if t != "4" && t != "5" && t != "ACHIEVEMENTS" && t != "GROUPACHIEVEMENTS" {
			continue
		}
		id, _ := strconv.ParseUint(blk.Name, 10, 32)
		for _, bit := range blk.Get("bits").Children {
			n, _ := strconv.Atoi(bit.Name)
			disp := bit.Get("display")
			a := Achievement{
				ID:        bit.Get("name").Str(),
				Name:      localized(disp.Get("name"), lang),
				Desc:      localized(disp.Get("desc"), lang),
				Hidden:    disp.Get("hidden").Str() == "1",
				Protected: bit.Get("permission").Int()&3 != 0 || blk.Get("permission").Int()&3 != 0,
				block:     uint32(id),
				bit:       uint(n),
			}
			if icon := disp.Get("icon").Str(); icon != "" {
				a.Icon = fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steamcommunity/public/images/apps/%d/%s", appID, icon)
			}
			a.Unlocked = gs.blocks[a.block]&(1<<a.bit) != 0
			if ts := unlockTimes[a.block]; a.Unlocked && int(a.bit) < len(ts) {
				a.UnlockedAt = int64(ts[a.bit])
			}
			if a.Name == "" {
				a.Name = a.ID
			}
			gs.Achievements = append(gs.Achievements, a)
		}
	}
	if len(gs.Achievements) == 0 {
		return nil, errors.New("this game has no achievements")
	}
	return gs, nil
}

func localized(k *KV, lang string) string {
	if k == nil {
		return ""
	}
	if len(k.Children) == 0 {
		return k.Value
	}
	want := map[string]string{"ru": "russian", "en": "english"}[lang]
	for _, l := range []string{want, "english"} {
		if v := k.Get(l).Str(); v != "" {
			return v
		}
	}
	return k.Children[0].Value
}

// SetAchievements unlocks (true) or locks (false) achievements by id. Protected ones are skipped.
func (c *Client) SetAchievements(gs *GameStats, steamID uint64, want map[string]bool) error {
	changed := map[uint32]uint32{}
	for _, a := range gs.Achievements {
		v, ok := want[a.ID]
		if !ok || a.Protected || v == a.Unlocked {
			continue
		}
		mask, seen := changed[a.block]
		if !seen {
			mask = gs.blocks[a.block]
		}
		if v {
			mask |= 1 << a.bit
		} else {
			mask &^= 1 << a.bit
		}
		changed[a.block] = mask
	}
	if len(changed) == 0 {
		return nil
	}
	ids := make([]uint32, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	req := NewPB().Fixed64(1, uint64(gs.AppID)).Fixed64(2, steamID).Fixed64(3, steamID).Uint(4, uint64(gs.crc)).Bool(5, false)
	for _, id := range ids {
		req.Msg(6, NewPB().Uint(1, uint64(id)).Uint(2, uint64(changed[id])))
	}
	p, err := c.RequestReply(EMsgClientStoreUserStats2, req, EMsgClientStoreUserStatsResponse, 20*time.Second)
	if err != nil {
		return err
	}
	m := p.Fields()
	if res := int(m.I(2)); res != ResOK {
		return fmt.Errorf("%s", ResultName(res))
	}
	gs.crc = uint32(m.U(3))
	for id, mask := range changed {
		gs.blocks[id] = mask
	}
	for i := range gs.Achievements {
		a := &gs.Achievements[i]
		a.Unlocked = gs.blocks[a.block]&(1<<a.bit) != 0
	}
	return nil
}
