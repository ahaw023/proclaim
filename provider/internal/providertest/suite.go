package providertest

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dogmatiq/dissolve/dnssd"
	"github.com/dogmatiq/proclaim/provider"
	"github.com/miekg/dns"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const (
	testTimeout     = 10 * time.Minute
	convergeTimeout = 5 * time.Minute
)

// TestContext contains provider-specific testing-related information.
type TestContext struct {
	Provider      provider.Provider
	Domain        string
	NameServers   func(ctx context.Context) ([]string, error)
	DeleteRecords func(ctx context.Context) error
}

// DeclareTestSuite declares a Ginkgo test suite for a provider implementation.
func DeclareTestSuite(
	setUp func(context.Context) TestContext,
) {
	ginkgo.Describe("Provider", func() {
		var (
			ctx      context.Context
			tctx     TestContext
			resolver *dnssd.UnicastResolver
			service  string
		)

		ginkgo.BeforeEach(func() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
			ginkgo.DeferCleanup(cancel)

			service = fmt.Sprintf(
				"_%d_%d._udp",
				os.Getpid(),
				time.Now().Unix(),
			)

			tctx = setUp(ctx)

			servers, err := tctx.NameServers(ctx)
			gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			gomega.Expect(servers).ShouldNot(gomega.BeEmpty())

			resolver = &dnssd.UnicastResolver{
				Config: &dns.ClientConfig{
					Port:     "53",
					Ndots:    1,
					Timeout:  5,
					Attempts: 10,
					Servers:  servers,
				},
			}

			err = tctx.DeleteRecords(ctx)
			gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.When("the provider can not advertise on the domain", func() {
			ginkgo.Describe("func AdvertiserByDomain()", func() {
				ginkgo.It("returns false", func() {
					_, ok, err := tctx.Provider.AdvertiserByDomain(ctx, "non-existent."+tctx.Domain)
					gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					gomega.Expect(ok).To(gomega.BeFalse())
				})
			})
		})

		ginkgo.When("the provider can advertise on the domain", func() {
			var advertiser provider.Advertiser

			ginkgo.BeforeEach(func() {
				var (
					ok  bool
					err error
				)
				advertiser, ok, err = tctx.Provider.AdvertiserByDomain(ctx, tctx.Domain)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ok).To(gomega.BeTrue())
			})

			ginkgo.It("can advertise and unadvertise instances", func() {
				expect := []dnssd.ServiceInstance{
					{
						Name:        "instance-1",
						ServiceType: service,
						Domain:      tctx.Domain,
						TargetHost:  "host1.example.com",
						TargetPort:  1000,
						Priority:    100,
						Weight:      10,
						TTL:         1 * time.Second,
					},
					{
						Name:        "instance-2",
						ServiceType: service,
						Domain:      tctx.Domain,
						TargetHost:  "host2.example.com",
						TargetPort:  2000,
						Priority:    200,
						Weight:      20,
						TTL:         2 * time.Second,
					},
				}

				for i, inst := range expect {
					cs, err := advertiser.Advertise(ctx, inst)
					gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					gomega.Expect(cs.IsCreate()).To(gomega.BeTrue())

					expectInstanceToEventuallyEqual(ctx, resolver, inst)
					expectInstanceListToEventuallyEqual(ctx, resolver, service, tctx.Domain, expect[:i+1]...)
				}

				// Check that all instances still exist after they have all the
				// advertise calls.
				for _, inst := range expect {
					expectInstanceToEventuallyEqual(ctx, resolver, inst)
				}

				expectInstanceListToEventuallyEqual(ctx, resolver, service, tctx.Domain, expect...)

				for i, inst := range expect {
					cs, err := advertiser.Unadvertise(ctx, inst)
					gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					gomega.Expect(cs.IsEmpty()).To(gomega.BeFalse())

					expectInstanceToEventuallyEqual(ctx, resolver, inst)
					expectInstanceListToEventuallyEqual(ctx, resolver, service, tctx.Domain, expect[i+1:]...)
				}

				expectInstanceListToEventuallyEqual(ctx, resolver, service, tctx.Domain)
			})

			ginkgo.It("can update an existing instance", func() {
				before := dnssd.ServiceInstance{
					Name:        "instance",
					ServiceType: service,
					Domain:      tctx.Domain,
					TargetHost:  "host.example.com",
					TargetPort:  443,
					Priority:    10,
					Weight:      20,
					TTL:         5 * time.Second,
					Attributes: []dnssd.Attributes{
						dnssd.
							NewAttributes().
							WithPair("key", []byte("value")),
					},
				}

				_, err := advertiser.Advertise(ctx, before)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				after := dnssd.ServiceInstance{
					Name:        "instance",
					ServiceType: service,
					Domain:      tctx.Domain,
					TargetHost:  "updated.example.com",
					TargetPort:  444,
					Priority:    11,
					Weight:      21,
					TTL:         6 * time.Second,
					Attributes: []dnssd.Attributes{
						dnssd.
							NewAttributes().
							WithPair("key", []byte("updated")),
					},
				}

				cs, err := advertiser.Advertise(ctx, after)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(cs.IsCreate()).To(gomega.BeFalse())
				gomega.Expect(cs.IsEmpty()).To(gomega.BeFalse())

				expectInstanceToEventuallyEqual(ctx, resolver, after)
			})

			ginkgo.It("ignores an existing identical instance", func() {
				expect := dnssd.ServiceInstance{
					Name:        "instance",
					ServiceType: service,
					Domain:      tctx.Domain,
					TargetHost:  "host.example.com",
					TargetPort:  443,
					Priority:    10,
					Weight:      20,
					TTL:         5 * time.Second,
					Attributes: []dnssd.Attributes{
						dnssd.
							NewAttributes().
							WithPair("key", []byte("value")),
					},
				}

				_, err := advertiser.Advertise(ctx, expect)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				cs, err := advertiser.Advertise(ctx, expect)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(cs.IsEmpty()).To(gomega.BeTrue())

				expectInstanceToEventuallyEqual(ctx, resolver, expect)
			})

			ginkgo.It("does not fail when unadvertising a non-existent instance", func() {
				inst := dnssd.ServiceInstance{
					Name:        "instance",
					ServiceType: service,
					Domain:      tctx.Domain,
					TargetHost:  "host.example.com",
					TargetPort:  443,
					Priority:    10,
					Weight:      20,
					TTL:         5 * time.Second,
					Attributes: []dnssd.Attributes{
						dnssd.
							NewAttributes().
							WithPair("key", []byte("value")),
					},
				}

				cs, err := advertiser.Unadvertise(ctx, inst)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(cs.IsEmpty()).To(gomega.BeTrue())
			})
		})
	})
}
