package dnfbridge

import (
	"errors"

	dnfrental "longheng.io/server/internal/modules/dnf/rental"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentRentalAssetOwner(repositories dnfrepo.Group) (*dnfrental.Owner, error) {
	owner, err := dnfrental.NewOwner(repositories)
	if err != nil {
		return nil, dnfrepo.ErrRentalAssetTransactionUnavailable
	}
	return owner, nil
}

func currentRentalMutationError(err error) error {
	switch {
	case errors.Is(err, dnfrental.ErrOwnerUnavailable),
		errors.Is(err, dnfrental.ErrAccountRequired),
		errors.Is(err, dnfrental.ErrCharacterRequired),
		errors.Is(err, dnfrental.ErrProjectorRequired),
		errors.Is(err, dnfrental.ErrCommandInvalid):
		return errors.Join(dnfrepo.ErrRentalAssetTransactionUnavailable, err)
	default:
		return err
	}
}
