package anonymizer

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"new-router/common"
)

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	awsARNPrefix = regexp.MustCompile(`^arn:aws:([^:]+):([^:]*):(\d{12}):(.+)$`)
)

var specialAddressScopes = []struct {
	prefix netip.Prefix
	kind   string
}{
	{netip.MustParsePrefix("192.0.2.0/24"), "documentation"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentation"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentation"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation"},
	{netip.MustParsePrefix("100.64.0.0/10"), "shared"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmark"},
	{netip.MustParsePrefix("2001:2::/48"), "benchmark"},
	{netip.MustParsePrefix("0.0.0.0/8"), "reserved"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved"},
}

// surrogateEntropy draws the bounded integers that shape a surrogate.
// cryptoEntropy produces a fresh unpredictable value per request; a
// deterministicEntropy stream replays identically for the same salt and info,
// which keeps stable pseudonyms byte-identical across requests.
type surrogateEntropy interface {
	Int(min, max int) int
}

type cryptoEntropy struct{}

func (cryptoEntropy) Int(min, max int) int {
	if max <= min {
		return min
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return min
	}
	return min + int(n.Int64())
}

// deterministicEntropy expands HMAC(salt, info||counter) into a byte stream.
// Both sides of a conversation derive the same stream from the same inputs;
// without the salt the mapping stays unguessable.
type deterministicEntropy struct {
	salt    []byte
	info    []byte
	buf     []byte
	offset  int
	counter uint32
}

func (d *deterministicEntropy) refill() {
	d.counter++
	mac := hmac.New(sha256.New, d.salt)
	mac.Write(d.info)
	var counter [4]byte
	binary.BigEndian.PutUint32(counter[:], d.counter)
	mac.Write(counter[:])
	d.buf = mac.Sum(nil)
	d.offset = 0
}

func (d *deterministicEntropy) Int(min, max int) int {
	if max <= min {
		return min
	}
	for len(d.buf)-d.offset < 4 {
		d.refill()
	}
	value := binary.BigEndian.Uint32(d.buf[d.offset : d.offset+4])
	d.offset += 4
	return min + int(uint64(value)%uint64(max-min+1))
}

// surrogateRng shapes surrogate characters from one entropy source.
type surrogateRng struct {
	entropy surrogateEntropy
}

func (r surrogateRng) choice(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[r.entropy.Int(0, len(values)-1)]
}

func (r surrogateRng) lower(n int) string { return r.from("abcdefghijklmnopqrstuvwxyz", n) }
func (r surrogateRng) upperNum(n int) string {
	return r.from("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", n)
}
func (r surrogateRng) alphaNum(n int) string {
	return r.from("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", n)
}
func (r surrogateRng) base64URL(n int) string {
	return r.from("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-", n)
}
func (r surrogateRng) hex(n int) string    { return r.from("abcdef0123456789", n) }
func (r surrogateRng) digits(n int) string { return r.from("0123456789", n) }

func (r surrogateRng) from(alphabet string, n int) string {
	if n <= 0 || alphabet == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		b.WriteByte(alphabet[r.entropy.Int(0, len(alphabet)-1)])
	}
	return b.String()
}

// GenerateSurrogate returns a realistic but fake replacement for an entity.
func GenerateSurrogate(original string, entityType string) string {
	return generateSurrogate(surrogateRng{cryptoEntropy{}}, original, entityType)
}

// GenerateStableSurrogate returns the deterministic surrogate for one salt,
// original, entity type and retry attempt. Equal inputs always produce the
// equal replacement, so full-history requests replay identical prefixes.
func GenerateStableSurrogate(salt []byte, original string, entityType string, attempt int) string {
	info := fmt.Appendf(nil, "%s\x00%s\x00%d", entityType, original, attempt)
	return generateSurrogate(surrogateRng{&deterministicEntropy{salt: salt, info: info}}, original, entityType)
}

// stableSurrogateSalt derives the engagement-scoped salt from the instance
// secret. Scoping to one user and token stops the same original from linking
// unrelated clients; rotating CRYPTO_SECRET rotates every pseudonym.
func stableSurrogateSalt(engagement string) []byte {
	master := sha256.Sum256([]byte("NewRouter anonymization stable surrogates v1\x00" + common.CryptoSecret))
	mac := hmac.New(sha256.New, master[:])
	mac.Write([]byte(engagement))
	return mac.Sum(nil)
}

func generateSurrogate(rng surrogateRng, original string, entityType string) string {
	switch entityType {
	case "IP_ADDRESS":
		return fakeIP(rng, original)
	case "CIDR":
		return fakeCIDR(rng, original)
	case "HOSTNAME":
		return fmt.Sprintf("%s-%04d", rng.choice([]string{"srv", "host", "node", "dc", "box", "ws", "app", "db"}), rng.entropy.Int(0, 9999))
	case "DOMAIN":
		return rng.lower(8) + ".pentest.local"
	case "USERNAME":
		return "user_" + rng.lower(6)
	case "EMAIL_ADDRESS", "EMAIL":
		return rng.lower(8) + "@example.pentest"
	case "URL":
		return fakeURL(rng, original)
	case "ORGANIZATION":
		return "[ORG_" + rng.upperNum(12) + "]"
	case "PERSON":
		return "[PERSON_" + rng.upperNum(12) + "]"
	case "PHONE_NUMBER":
		return "[PHONE_" + rng.upperNum(12) + "]"
	case "CN_ID_CARD":
		return "[CNID_" + rng.upperNum(12) + "]"
	case "ADDRESS", "LOCATION":
		return "[ADDRESS_" + rng.upperNum(12) + "]"
	case "CUSTOM":
		return "[CUSTOM_" + rng.upperNum(12) + "]"
	case "CREDENTIAL":
		return "[CRED_" + rng.upperNum(8) + "]"
	case "HASH":
		return rng.hex(len(original))
	case "MAC_ADDRESS":
		parts := make([]string, 6)
		for i := range parts {
			parts[i] = fmt.Sprintf("%02x", rng.entropy.Int(0, 255))
		}
		return strings.Join(parts, ":")
	case "PATH":
		return "/home/user_" + rng.lower(5) + "/data"
	case "TOKEN":
		return fakeToken(rng, original)
	case "IDENTIFIER":
		return fakeIdentifier(rng, original)
	default:
		return "[REDACTED_" + rng.upperNum(6) + "]"
	}
}

func addressKind(address netip.Addr) string {
	switch {
	case address.IsUnspecified():
		return "unspecified"
	case address.IsLoopback():
		return "loopback"
	case address.IsLinkLocalUnicast():
		return "link-local"
	case address.IsMulticast():
		return "multicast"
	case address.IsPrivate():
		return "private"
	default:
		for _, scope := range specialAddressScopes {
			if scope.prefix.Contains(address) {
				return scope.kind
			}
		}
		return "public"
	}
}

func fakeIP(rng surrogateRng, original string) string {
	address, err := netip.ParseAddr(original)
	if err != nil {
		return "ip-" + rng.lower(12)
	}
	mapped := address.Is4In6()
	address = address.Unmap()
	kind := addressKind(address)
	if address.Is4() {
		var replacement string
		switch kind {
		case "private":
			// The 10/8 space retains private-network semantics and enough entropy
			// for the bounded vault to resolve collisions with real input addresses.
			replacement = fmt.Sprintf("10.%d.%d.%d", rng.entropy.Int(0, 255), rng.entropy.Int(0, 255), rng.entropy.Int(1, 254))
		case "loopback":
			replacement = fmt.Sprintf("127.%d.%d.%d", rng.entropy.Int(0, 255), rng.entropy.Int(0, 255), rng.entropy.Int(1, 254))
		case "link-local":
			replacement = fmt.Sprintf("169.254.%d.%d", rng.entropy.Int(1, 254), rng.entropy.Int(1, 254))
		default:
			family := "ip"
			if mapped {
				family = "ipv4-mapped"
			}
			// A named stand-in preserves reachability semantics without directing
			// the provider toward a randomly selected real public address.
			return kind + "-" + family + "-" + rng.lower(12)
		}
		if mapped {
			return "::ffff:" + replacement
		}
		return replacement
	}
	if kind != "private" && kind != "link-local" {
		return kind + "-ipv6-" + rng.lower(12)
	}
	var bytes [16]byte
	for i := range bytes {
		bytes[i] = byte(rng.entropy.Int(0, 255))
	}
	bytes[0] = 0xfd
	if kind == "link-local" {
		bytes[0], bytes[1] = 0xfe, 0x80
		clear(bytes[2:8])
	}
	return netip.AddrFrom16(bytes).String()
}

func fakeCIDR(rng surrogateRng, original string) string {
	prefix, err := netip.ParsePrefix(original)
	if err != nil {
		return "network-" + rng.lower(12)
	}
	address := prefix.Addr()
	// Narrow private networks retain valid CIDR notation. Broad or public
	// networks use a named stand-in so masking cannot create a real route or
	// exhaust the few available networks in their original prefix class.
	if address.IsPrivate() && !address.Is4In6() && prefix.Bits() >= 24 {
		replacement, err := netip.ParseAddr(fakeIP(rng, address.String()))
		if err == nil {
			return netip.PrefixFrom(replacement, prefix.Bits()).Masked().String()
		}
	}
	family := "ipv6"
	if address.Is4() {
		family = "ipv4"
	}
	return fmt.Sprintf("%s-%s-network-%s/%d", addressKind(address.Unmap()), family, rng.lower(12), prefix.Bits())
}

func fakeURL(rng surrogateRng, original string) string {
	parsed, err := url.Parse(original)
	if err != nil || parsed.Hostname() == "" {
		return "http://host-" + rng.lower(12) + ".invalid/path"
	}
	host := "host-" + rng.lower(12) + ".invalid"
	if address, err := netip.ParseAddr(parsed.Hostname()); err == nil {
		host = fakeIP(rng, address.String())
	} else if strings.EqualFold(parsed.Hostname(), "localhost") {
		host = "loopback-host-" + rng.lower(12)
	}
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	} else if address, err := netip.ParseAddr(host); err == nil && address.Is6() {
		host = "[" + host + "]"
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: host, Path: "/path"}).String()
}

func fakeToken(rng surrogateRng, original string) string {
	awsPrefixes := []string{"AKIA", "ASIA", "AROA", "AIPA", "ANPA", "ANVA", "APKA", "AIDA"}
	for _, prefix := range awsPrefixes {
		if strings.HasPrefix(original, prefix) && len(original) >= 20 {
			return prefix + rng.upperNum(len(original)-len(prefix))
		}
	}
	if strings.HasPrefix(original, "eyJ") && strings.Count(original, ".") == 2 {
		parts := strings.Split(original, ".")
		return parts[0] + "." + rng.base64URL(len(parts[1])) + "." + rng.base64URL(len(parts[2]))
	}
	return "[TOKEN_" + rng.alphaNum(32) + "]"
}

func fakeIdentifier(rng surrogateRng, original string) string {
	if uuidPattern.MatchString(original) {
		return rng.hex(8) + "-" + rng.hex(4) + "-4" + rng.hex(3) + "-" + rng.choice([]string{"8", "9", "a", "b"}) + rng.hex(3) + "-" + rng.hex(12)
	}
	if regexp.MustCompile(`^\d{12}$`).MatchString(original) {
		return rng.digits(12)
	}
	if match := awsARNPrefix.FindStringSubmatch(original); match != nil {
		return fmt.Sprintf("arn:aws:%s:%s:%s:%s", match[1], match[2], rng.digits(12), "resource/"+rng.lower(8))
	}
	if strings.HasPrefix(original, "+55") {
		return fmt.Sprintf("+55 %02d 9%s-%s", rng.entropy.Int(11, 99), rng.digits(4), rng.digits(4))
	}
	return "[REDACTED_" + rng.upperNum(6) + "]"
}
