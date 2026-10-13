package anonymizer

import (
	"crypto/rand"
	"fmt"
	"math/big"
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
		return randomChoice([]string{"Northwind Labs", "Apex Systems", "Vertex Group", "Praxis Security", "Helios Digital"})
	case "PERSON":
		return randomChoice([]string{"Alex Morgan", "Jordan Taylor", "Casey Brooks", "Riley Parker", "Morgan Ellis"})
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
	net := randomChoice([]string{"10.200.99", "10.201.88", "172.28.199"})
	return fmt.Sprintf("%s.%d", net, randomInt(1, 254))
}

func fakeCIDR() string {
	net := randomChoice([]string{"10.200.99", "10.201.88", "172.28.199"})
	return fmt.Sprintf("%s.0/%d", net, randomInt(16, 30))
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
