package dnfbridge

import (
	"context"

	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// currentAccountCera reads the account-shared Cera balance from account
// metadata. The balance lives in dnf_accounts.metadata_json["account_cera"]
// so every character on the account sees and spends the same pool; the
// per-character dnf_characters.cera column is legacy read-only data.
func currentAccountCera(account dnfrepo.AccountRecord) int64 {
	return dnfcerashop.Balance(account)
}

// setCurrentAccountCera writes the account-shared Cera balance back through
// the same metadata key. Negative inputs clamp to zero so a malformed caller
// can never produce a negative shared pool.
func setCurrentAccountCera(account *dnfrepo.AccountRecord, balance int64) {
	dnfcerashop.SetBalance(account, balance)
}

// loadCurrentAccountCera is the read-only display path for places that only
// need the current shared balance without joining a rental-assets mutation.
func (s *Service) loadCurrentAccountCera(ctx context.Context, repositories dnfrepo.Group, sessions ...*gameSession) int64 {
	if repositories.Account == nil {
		return 0
	}
	account, found, err := repositories.Account.Load(ctx, s.accountIDForSession(sessions...))
	if err != nil || !found {
		return 0
	}
	return currentAccountCera(account)
}
