package doh

import (
	"context"
	"regexp"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes doh as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/doh-cli/doh"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// doh:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone doh binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the doh driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "doh",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "doh",
			Short:  "DNS over HTTPS: query any record type from the command line.",
			Long: `DNS over HTTPS: query any record type from the command line.

doh resolves domains and IPs using Google's public DoH API (dns.google/resolve)
over plain HTTPS, shapes the results into clean records, and prints output that
pipes into the rest of your tools. No API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/doh-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// resolve op: list DNS records for a domain or IP.
	kit.Handle(app, kit.OpMeta{Name: "resolve", Group: "read", List: true,
		Summary: "Resolve a domain or IP address",
		Args:    []kit.Arg{{Name: "domain", Help: "domain name or IP address"}},
	}, resolveOp)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type resolveIn struct {
	Domain string  `kit:"arg"          help:"domain name or IP address"`
	Type   string  `kit:"flag"         help:"DNS record type (A, AAAA, MX, TXT, NS, CNAME, SOA, PTR)" default:"A"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func resolveOp(ctx context.Context, in resolveIn, emit func(*Record) error) error {
	recordType := in.Type
	if recordType == "" {
		recordType = "A"
	}

	// auto-detect IPs and use PTR
	if isIP(in.Domain) && in.Type == "" {
		recordType = "PTR"
	}

	records, err := in.Client.Resolve(ctx, in.Domain, recordType)
	if err != nil {
		return mapErr(err)
	}

	count := 0
	for i := range records {
		if in.Limit > 0 && count >= in.Limit {
			break
		}
		if err := emit(&records[i]); err != nil {
			return err
		}
		count++
	}
	return nil
}

// --- Resolver: the URI-native string functions, pure and network-free ---

var ipRE = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)

func isIP(s string) bool {
	return ipRE.MatchString(s)
}

// Classify turns any accepted input into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	if input == "" {
		return "", "", errs.Usage("doh: domain or IP required")
	}
	if isIP(input) {
		return "ip", input, nil
	}
	return "domain", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "domain":
		return BaseURL + "/resolve?name=" + id + "&type=A", nil
	case "ip":
		return BaseURL + "/resolve?name=" + id + "&type=PTR", nil
	default:
		return "", errs.Usage("doh has no resource type %q", uriType)
	}
}

// --- helpers ---

func mapErr(err error) error {
	return err
}
