package player

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Record 是兼容旧玩家模块调用方的 Profile 别名。
type Record = Profile

func (m *Module) mutateLoadedProfile(ctx context.Context, accountID string, mutate func(*Profile) []ProfileField) (Profile, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Profile{}, fmt.Errorf("account id is required")
	}
	if mutate == nil {
		return Profile{}, fmt.Errorf("profile mutation is required")
	}
	if _, err := m.LoadOrCreate(ctx, accountID); err != nil {
		return Profile{}, err
	}
	before, profile, err := m.profiles.MutateLoaded(ctx, accountID, func(profile *Profile, _ time.Time) ([]ProfileField, error) {
		return mutate(profile), nil
	})
	if err != nil {
		return Profile{}, err
	}
	m.observeSummaryChange(ctx, before, profile)
	return cloneProfile(profile), nil
}

func (m *Module) mutateCritProfile(ctx context.Context, accountID string, operation ProfileOperation, reason string, mutate func(*Profile, time.Time) (map[string]any, error)) (Profile, error) {
	if m == nil {
		return Profile{}, fmt.Errorf("profile module is nil")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Profile{}, fmt.Errorf("account id is required")
	}
	if mutate == nil {
		return Profile{}, fmt.Errorf("profile mutation is required")
	}
	dirtyFields := dirtyFieldsByOp(operation)
	if len(dirtyFields) == 0 {
		return Profile{}, fmt.Errorf("unsupported profile operation %s", operation)
	}
	before, profile, err := m.profiles.MutateLoaded(ctx, accountID, func(profile *Profile, now time.Time) ([]ProfileField, error) {
		mutation, err := mutate(profile, now)
		if err != nil {
			return nil, err
		}
		eventProfile := cloneProfile(*profile)
		eventProfile.UpdatedAt = now
		eventProfile.Version++
		if err := m.emitProfileMutation(ctx, eventProfile, string(operation), reason, dirtyFields, mutation); err != nil {
			return nil, err
		}
		return dirtyFields, nil
	})
	if err != nil {
		return Profile{}, err
	}
	m.observeSummaryChange(ctx, before, profile)
	return cloneProfile(profile), nil
}

func newProfile(accountID string, now time.Time) Profile {
	profile := Profile{
		AccountID: accountID,
		RoleID:    "role-" + accountID,
		State:     "online",
		LoadedAt:  now,
		UpdatedAt: now,
		Version:   1,
	}
	applyProfileDefaults(&profile, now)
	return profile
}

func prepareLoadedProfile(profile Profile, accountID string, now time.Time, existed bool) (Profile, []ProfileField, error) {
	dirtyFields := newProfileFieldSet(dirtyFieldsByModule(ProfileModuleRuntime)...)
	if existed {
		profile = normAccountID(profile, accountID)
		dirtyFields.Merge(ensureProfileModules(&profile, now))
	}
	profile.State = "online"
	profile.LoadedAt = now
	profile.UpdatedAt = now
	profile.Version++
	return profile, dirtyFields.List(), nil
}

func prepareSavedProfile(profile Profile, now time.Time) (Profile, []ProfileField, error) {
	profile.State = "offline"
	profile.SavedAt = now
	profile.UpdatedAt = now
	profile.Version++
	return profile, dirtyFieldsByModule(ProfileModuleRuntime), nil
}

func touchProfileRecord(profile Profile, now time.Time) (Profile, []ProfileField, error) {
	profile.UpdatedAt = now
	profile.Version++
	return profile, nil, nil
}

func sameProfileVersion(current, snapshot Profile) bool {
	return current.Version == snapshot.Version && current.UpdatedAt.Equal(snapshot.UpdatedAt)
}

func ensureProfileModules(profile *Profile, now time.Time) profileFieldSet {
	return newProfileFieldSet(backfillModules(profile, now)...)
}

func cloneProfile(profile Profile) Profile {
	return cloneProfileModules(profile)
}
