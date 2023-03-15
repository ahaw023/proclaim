package dnsimpleprovider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dnsimple/dnsimple-go/dnsimple"
	"github.com/dogmatiq/dissolve/dnssd"
	"github.com/dogmatiq/proclaim/provider"
)

type advertiser struct {
	API  *dnsimple.ZonesService
	Zone *dnsimple.Zone
}

func (a *advertiser) ID() string {
	return marshalAdvertiserID(a.Zone)
}

func (a *advertiser) Advertise(
	ctx context.Context,
	inst dnssd.ServiceInstance,
) (provider.AdvertiseResult, error) {
	cs := &changeSet{}

	if err := a.syncPTR(ctx, inst, cs); err != nil {
		return provider.AdvertiseError, err
	}

	if err := a.syncSRV(ctx, inst, cs); err != nil {
		return provider.AdvertiseError, err
	}

	if err := a.syncTXT(ctx, inst, cs); err != nil {
		return provider.AdvertiseError, err
	}

	creates, updates, deletes, err := a.applyChangeSet(ctx, cs)
	if err != nil {
		return provider.AdvertiseError, err
	}

	switch {
	case updates != 0 || deletes != 0:
		return provider.UpdatedExistingInstance, nil
	case creates == 0:
		return provider.InstanceAlreadyAdvertised, nil
	case creates == 2+len(inst.Attributes): // PTR + SRV + count(TXT)
		return provider.AdvertisedNewInstance, nil
	default:
		return provider.UpdatedExistingInstance, nil
	}
}

func (a *advertiser) Unadvertise(
	ctx context.Context,
	inst dnssd.ServiceInstance,
) (provider.UnadvertiseResult, error) {
	cs := &changeSet{}

	if err := a.deletePTR(ctx, inst, cs); err != nil {
		return provider.UnadvertiseError, err
	}

	if err := a.deleteSRV(ctx, inst, cs); err != nil {
		return provider.UnadvertiseError, err
	}

	if err := a.deleteTXT(ctx, inst, cs); err != nil {
		return provider.UnadvertiseError, err
	}

	_, _, deletes, err := a.applyChangeSet(ctx, cs)
	if err != nil {
		return provider.UnadvertiseError, err
	}

	switch deletes {
	case 0:
		return provider.InstanceNotAdvertised, nil
	default:
		return provider.UnadvertisedExistingInstance, nil
	}
}

func (a *advertiser) applyChangeSet(
	ctx context.Context,
	cs *changeSet,
) (creates, updates, deletes int, err error) {
	accountID := strconv.FormatInt(a.Zone.AccountID, 10)

	for _, rec := range cs.deletes {
		if _, err := a.API.DeleteRecord(ctx, accountID, a.Zone.Name, rec.ID); err != nil {
			return 0, 0, 0, fmt.Errorf("unable to delete %s record: %w", rec.Type, err)
		}
	}

	for _, up := range cs.updates {
		if _, err := a.API.UpdateRecord(ctx, accountID, a.Zone.Name, up.Before.ID, up.After); err != nil {
			return 0, 0, 0, fmt.Errorf("unable to update %s record: %w", up.Before.Type, err)
		}
	}

	for _, attr := range cs.creates {
		if _, err := a.API.CreateRecord(ctx, accountID, a.Zone.Name, attr); err != nil {
			return 0, 0, 0, fmt.Errorf("unable to create %s record: %w", attr.Type, err)
		}
	}

	return len(cs.creates), len(cs.updates), len(cs.deletes), nil
}
