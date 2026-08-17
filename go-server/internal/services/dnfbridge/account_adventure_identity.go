package dnfbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) currentRepresentAccountIdentity(
	ctx context.Context,
	sessions ...*gameSession,
) (string, uint32, string, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil {
		return "", 0, "unavailable_zero", dnfrepo.ErrRepoMissing
	}
	account, found, err := repositories.Account.Load(ctx, strings.TrimSpace(s.accountIDForSession(sessions...)))
	if err != nil {
		return "", 0, "unavailable_zero", err
	}
	if !found {
		return "", 0, "unavailable_zero", nil
	}
	createdDate, source, err := adventureGroupCreatedDisplayDate(account)
	if err != nil {
		return "", 0, source, err
	}
	return strings.TrimSpace(account.RepresentAccountName), createdDate, source, nil
}

func adventureGroupCreatedDisplayDate(
	account dnfrepo.AccountRecord,
) (uint32, string, error) {
	value := strings.TrimSpace(account.Metadata[adventureGroupCreatedDateMetadataKey])
	if value == "" {
		return 0, "unavailable_zero", nil
	}
	created, err := time.Parse(adventureGroupCreatedDateLayout, value)
	if err != nil {
		return 0, "account_metadata_invalid", fmt.Errorf(
			"parse %s %q: %w",
			adventureGroupCreatedDateMetadataKey,
			value,
			err,
		)
	}
	year, month, day := created.Date()
	if year < 1 || year > 9999 {
		return 0, "account_metadata_invalid", fmt.Errorf(
			"%s year %d is outside 1..9999",
			adventureGroupCreatedDateMetadataKey,
			year,
		)
	}
	return uint32(year*10000 + int(month)*100 + day), "account_metadata", nil
}
