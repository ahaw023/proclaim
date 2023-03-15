package route53provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/dogmatiq/proclaim/provider"
)

// Provider is an implementation of provider.Provider that advertises DNS-SD
// services on domains hosted by Amazon Route 53.
type Provider struct {
	PartitionID string
	Client      *route53.Client
}

// ID returns a short unique identifier for the provider.
func (p *Provider) ID() string {
	if p.PartitionID == "" {
		panic("partition must not be empty")
	}
	return fmt.Sprintf("route53/%s", p.PartitionID)
}

// Describe returns a human-readable description of the provider.
func (p *Provider) Describe() string {
	return "Amazon Route 53"
}

// AdvertiserByID returns the Advertiser with the given ID.
func (p *Provider) AdvertiserByID(
	ctx context.Context,
	id string,
) (provider.Advertiser, error) {
	if _, err := p.Client.GetHostedZone(ctx,
		&route53.GetHostedZoneInput{
			Id: aws.String(id),
		},
	); err != nil {
		return nil, fmt.Errorf("unable to get hosted zone: %w", err)
	}

	return &advertiser{
		PartitionID: p.PartitionID,
		Client:      p.Client,
		ZoneID:      id,
	}, nil
}

// AdvertiserByDomain returns the Advertiser used to advertise services on
// the given domain.
//
// ok is false if this provider does not manage the given domain.
func (p *Provider) AdvertiserByDomain(
	ctx context.Context,
	domain string,
) (provider.Advertiser, bool, error) {
	domain += "."

	out, err := p.Client.ListHostedZonesByName(
		ctx,
		&route53.ListHostedZonesByNameInput{
			DNSName:  aws.String(domain),
			MaxItems: aws.Int32(1),
		},
	)
	if err != nil {
		return nil, false, fmt.Errorf("unable to list hosted zones: %w", err)
	}

	if len(out.HostedZones) == 0 {
		return nil, false, nil
	}

	zone := out.HostedZones[0]

	if *zone.Name != domain {
		return nil, false, nil
	}

	return &advertiser{
		PartitionID: p.PartitionID,
		Client:      p.Client,
		ZoneID:      *zone.Id,
	}, true, nil
}
