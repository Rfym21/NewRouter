package anonymizer

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/netip"
	"regexp"
	"strings"
)

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	awsARNPrefix = regexp.MustCompile(`^arn:aws:([^:]+):([^:]*):(\d{12}):(.+)$`)
)

// GenerateSurrogate returns a realistic but fake replacement for an entity.
func GenerateSurrogate(original string, entityType string) string {
	switch entityType {
	case "IP_ADDRESS":
		return fakeIP()
	case "CIDR":
		return fakeCIDR()
	case "HOSTNAME":
		return fmt.Sprintf("%s-%04d", randomChoice([]string{"srv", "host", "node", "dc", "box", "ws", "app", "db"}), randomInt(0, 9999))
	case "DOMAIN":
		return randomLower(8) + ".pentest.local"
	case "USERNAME":
		return "user_" + randomLower(6)
	case "EMAIL_ADDRESS", "EMAIL":
		return randomLower(8) + "@example.pentest"
	case "URL":
		return "http://" + fakeIP() + "/path"
	case "ORGANIZATION":
		return "[ORG_" + randomUpperNum(12) + "]"
	case "PERSON":
		return "[PERSON_" + randomUpperNum(12) + "]"
	case "PHONE_NUMBER":
		return "[PHONE_" + randomUpperNum(12) + "]"
	case "CN_ID_CARD":
		return "[CNID_" + randomUpperNum(12) + "]"
	case "ADDRESS", "LOCATION":
		return "[ADDRESS_" + randomUpperNum(12) + "]"
	case "CUSTOM":
		return "[CUSTOM_" + randomUpperNum(12) + "]"
	case "CREDENTIAL":
		return "[CRED_" + randomUpperNum(8) + "]"
	case "HASH":
		return randomHex(len(original))
	case "MAC_ADDRESS":
		parts := make([]string, 6)
		for i := range parts {
			parts[i] = fmt.Sprintf("%02x", randomInt(0, 255))
		}
		return strings.Join(parts, ":")
	case "PATH":
		return "/home/user_" + randomLower(5) + "/data"
	case "TOKEN":
		return fakeToken(original)
	case "IDENTIFIER":
		return fakeIdentifier(original)
	default:
		return "[REDACTED_" + randomUpperNum(6) + "]"
	}
}

func fakeIP() string {
	// The full private 10/8 space is large enough for bounded request vaults,
	// unlike a few /24 pools which could overlap all true addresses in a batch.
	return fmt.Sprintf("10.%d.%d.%d", randomInt(0, 255), randomInt(0, 255), randomInt(1, 254))
}

func fakeCIDR() string {
	address := netip.AddrFrom4([4]byte{10, byte(randomInt(0, 255)), byte(randomInt(0, 255)), byte(randomInt(0, 255))})
	return netip.PrefixFrom(address, randomInt(16, 30)).Masked().String()
}

func fakeToken(original string) string {
	awsPrefixes := []string{"AKIA", "ASIA", "AROA", "AIPA", "ANPA", "ANVA", "APKA", "AIDA"}
	for _, prefix := range awsPrefixes {
		if strings.HasPrefix(original, prefix) && len(original) >= 20 {
			return prefix + randomUpperNum(len(original)-len(prefix))
		}
	}
	if strings.HasPrefix(original, "eyJ") && strings.Count(original, ".") == 2 {
		parts := strings.Split(original, ".")
		return parts[0] + "." + randomBase64URL(len(parts[1])) + "." + randomBase64URL(len(parts[2]))
	}
	return "[TOKEN_" + randomAlphaNum(32) + "]"
}

func fakeIdentifier(original string) string {
	if uuidPattern.MatchString(original) {
		return randomHex(8) + "-" + randomHex(4) + "-4" + randomHex(3) + "-" + randomChoice([]string{"8", "9", "a", "b"}) + randomHex(3) + "-" + randomHex(12)
	}
	if regexp.MustCompile(`^\d{12}$`).MatchString(original) {
		return randomDigits(12)
	}
	if match := awsARNPrefix.FindStringSubmatch(original); match != nil {
		return fmt.Sprintf("arn:aws:%s:%s:%s:%s", match[1], match[2], randomDigits(12), "resource/"+randomLower(8))
	}
	if strings.HasPrefix(original, "+55") {
		return fmt.Sprintf("+55 %02d 9%s-%s", randomInt(11, 99), randomDigits(4), randomDigits(4))
	}
	return "[REDACTED_" + randomUpperNum(6) + "]"
}

func randomChoice(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[randomInt(0, len(values)-1)]
}

func randomInt(min int, max int) int {
	if max <= min {
		return min
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return min
	}
	return min + int(n.Int64())
}

func randomLower(n int) string {
	return randomFrom("abcdefghijklmnopqrstuvwxyz", n)
}

func randomUpperNum(n int) string {
	return randomFrom("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", n)
}

func randomAlphaNum(n int) string {
	return randomFrom("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", n)
}

func randomBase64URL(n int) string {
	return randomFrom("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-", n)
}

func randomHex(n int) string {
	return randomFrom("abcdef0123456789", n)
}

func randomDigits(n int) string {
	return randomFrom("0123456789", n)
}

func randomFrom(alphabet string, n int) string {
	if n <= 0 || alphabet == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		idx := randomInt(0, len(alphabet)-1)
		b.WriteByte(alphabet[idx])
	}
	return b.String()
}
