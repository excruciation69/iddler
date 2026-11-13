package bot

// Achievements: list and change them, optionally one by one with human-like pauses.

import (
	"errors"
	"math/rand"
	"sort"
	"time"

	"iddler/internal/steam"
)

// Games with VAC or ranked online play: hidden from the achievements list by default.
var vacGames = map[uint32]bool{
	730: true, 570: true, 440: true, 578080: true, 252490: true, 359550: true, 1172470: true,
	271590: true, 2357570: true, 230410: true, 4000: false, 10: true, 240: true, 1085660: true,
	252950: true, 304930: true, 218620: true, 1938090: true, 2139460: true,
}

type achJob struct {
	AppID   uint32   `json:"appid"`
	Game    string   `json:"game"`
	Pending []string `json:"pending"`
	Unlock  bool     `json:"unlock"`
	Done    int      `json:"done"`
	NextAt  int64    `json:"nextAt"` // unix time of the next unlock, for the countdown in the interface
	next    time.Time
	busy    bool
}

func (b *Bot) AchievementGames() []steam.OwnedGame {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []steam.OwnedGame
	for _, g := range b.games {
		if g.HasStats && (b.cfg.Ach.ShowVAC || !vacGames[g.AppID]) {
			out = append(out, g)
		}
	}
	return out
}

// Achievements loads the achievement list of a game.
func (b *Bot) Achievements(appID uint32, lang string) ([]steam.Achievement, error) {
	cm := b.conn()
	if cm == nil {
		return nil, errors.New("offline")
	}
	b.mu.Lock()
	sid := b.steamID
	b.mu.Unlock()
	gs, err := cm.UserStats(appID, sid, lang)
	if err != nil {
		return nil, err
	}
	pct := steam.AchievementPercents(appID)
	for i := range gs.Achievements {
		gs.Achievements[i].Percent = pct[gs.Achievements[i].ID]
	}
	b.mu.Lock()
	b.ach[appID] = gs
	b.mu.Unlock()
	return gs.Achievements, nil
}

// SetAchievements applies changes now, or queues them when the human mode is on.
func (b *Bot) SetAchievements(appID uint32, ids []string, unlock, human bool) error {
	b.mu.Lock()
	gs := b.ach[appID]
	cm, sid := b.cm, b.steamID
	b.mu.Unlock()
	if gs == nil || cm == nil {
		return errors.New("reload the list first")
	}
	if !human || !unlock || len(ids) == 1 {
		want := map[string]bool{}
		for _, id := range ids {
			want[id] = unlock
		}
		if err := cm.SetAchievements(gs, sid, want); err != nil {
			return err
		}
		b.log("ok", "log.achDone", itoa(len(ids)), b.gameNameLocked(appID))
		return nil
	}
	// common ones first, like a real playthrough
	pct := map[string]float64{}
	for _, a := range gs.Achievements {
		pct[a.ID] = a.Percent
	}
	sort.SliceStable(ids, func(i, j int) bool { return pct[ids[i]] > pct[ids[j]] })
	b.mu.Lock()
	b.achJob = &achJob{AppID: appID, Game: b.gameName(appID), Pending: ids, Unlock: true, next: time.Now()}
	b.log("info", "log.achQueued", itoa(len(ids)), b.achJob.Game)
	b.applyPlaying()
	b.changed()
	b.mu.Unlock()
	return nil
}

func (b *Bot) gameNameLocked(id uint32) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gameName(id)
}

func (b *Bot) CancelAchievements() {
	b.mu.Lock()
	b.achJob = nil
	b.applyPlaying()
	b.changed()
	b.mu.Unlock()
}

// achDelay is a random pause between unlocks within the configured range. Caller holds b.mu.
func (b *Bot) achDelay() time.Duration {
	lo, hi := max(b.cfg.Ach.MinDelay, 1), b.cfg.Ach.MaxDelay
	if hi < lo {
		hi = lo
	}
	return time.Duration(lo)*time.Minute + time.Duration(rand.Int63n(int64(hi-lo+1)*int64(time.Minute)))
}

func (b *Bot) achStep() {
	b.mu.Lock()
	job := b.achJob
	if job == nil || len(job.Pending) == 0 || b.cm == nil {
		b.mu.Unlock()
		return
	}
	cm, sid, gs := b.cm, b.steamID, b.ach[job.AppID]
	b.mu.Unlock()
	var err error
	if gs == nil {
		if gs, err = cm.UserStats(job.AppID, sid, "en"); err == nil {
			b.mu.Lock()
			b.ach[job.AppID] = gs
			b.mu.Unlock()
		}
	}
	id := job.Pending[0]
	if err == nil {
		err = cm.SetAchievements(gs, sid, map[string]bool{id: true})
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.achJob != job {
		return
	}
	job.busy = false
	if err != nil {
		b.log("warn", "log.achFailed", errKey(err))
		job.next = time.Now().Add(5 * time.Minute)
		return
	}
	job.Pending, job.Done = job.Pending[1:], job.Done+1
	if len(job.Pending) == 0 {
		b.log("ok", "log.achDone", itoa(job.Done), job.Game)
		b.achJob = nil
		b.applyPlaying()
	} else {
		job.next = time.Now().Add(b.achDelay())
	}
	b.changed()
}
