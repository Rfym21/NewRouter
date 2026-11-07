package vertex

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"strings"
	"sync"

	relaycommon "new-router/relay/common"
	"new-router/service"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"

	"fmt"
	"time"
)

type Credentials struct {
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	ClientID     string `json:"client_id"`
}

// accessTokenRefreshMargin keeps requests from starting with nearly expired tokens.
const accessTokenRefreshMargin = 5 * time.Minute

type cachedAccessToken struct {
	token     string
	identity  string
	expiresAt time.Time
}

var (
	accessTokens       sync.Map
	accessTokenFlights singleflight.Group
	fetchAccessToken   = requestAccessToken
)

func getAccessToken(a *Adaptor, info *relaycommon.RelayInfo) (string, error) {
	cacheKey := fmt.Sprintf("%d", info.ChannelId)
	if info.ChannelIsMultiKey {
		cacheKey = fmt.Sprintf("%d-%d", info.ChannelId, info.ChannelMultiKeyIndex)
	}
	creds := a.AccountCredentials
	// Replaced credentials must not reuse the previous service-account token.
	identity := creds.ClientEmail + "\x00" + creds.PrivateKeyID
	if token, ok := loadAccessToken(cacheKey, identity); ok {
		return token, nil
	}
	proxy := info.ChannelSetting.Proxy
	value, err, _ := accessTokenFlights.Do(cacheKey+"\x00"+identity, func() (any, error) {
		if token, ok := loadAccessToken(cacheKey, identity); ok {
			return token, nil
		}
		token, lifetime, err := fetchAccessToken(creds, proxy)
		if err != nil {
			return "", err
		}
		if lifetime > accessTokenRefreshMargin {
			accessTokens.Store(cacheKey, cachedAccessToken{
				token:     token,
				identity:  identity,
				expiresAt: time.Now().Add(lifetime - accessTokenRefreshMargin),
			})
		}
		return token, nil
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func loadAccessToken(cacheKey, identity string) (string, bool) {
	value, ok := accessTokens.Load(cacheKey)
	if !ok {
		return "", false
	}
	entry := value.(cachedAccessToken)
	if entry.identity != identity || !time.Now().Before(entry.expiresAt) {
		return "", false
	}
	return entry.token, true
}

func createSignedJWT(email, privateKeyPEM string) (string, error) {

	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, "-----BEGIN PRIVATE KEY-----", "")
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, "-----END PRIVATE KEY-----", "")
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, "\r", "")
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, "\n", "")
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, "\\n", "")

	block, _ := pem.Decode([]byte("-----BEGIN PRIVATE KEY-----\n" + privateKeyPEM + "\n-----END PRIVATE KEY-----"))
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block containing the private key")
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}

	rsaPrivateKey, ok := privateKey.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("not an RSA private key")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   email,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   "https://www.googleapis.com/oauth2/v4/token",
		"exp":   now.Add(time.Minute * 35).Unix(),
		"iat":   now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(rsaPrivateKey)
	if err != nil {
		return "", err
	}

	return signedToken, nil
}

// AcquireAccessToken obtains a service-account token through the channel proxy.
func AcquireAccessToken(creds Credentials, proxy string) (string, error) {
	token, _, err := requestAccessToken(creds, proxy)
	return token, err
}

func requestAccessToken(creds Credentials, proxy string) (string, time.Duration, error) {
	signedJWT, err := createSignedJWT(creds.ClientEmail, creds.PrivateKey)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create signed JWT: %w", err)
	}
	token, lifetime, err := exchangeJwtForAccessToken(signedJWT, proxy)
	if err != nil {
		return "", 0, fmt.Errorf("failed to exchange JWT for access token: %w", err)
	}
	return token, lifetime, nil
}

// exchangeJwtForAccessToken trades a signed service-account JWT for an access
// token at Google's OAuth endpoint. It goes through the channel proxy only and
// always verifies the certificate; the channel's TLS setting does not apply.
func exchangeJwtForAccessToken(signedJWT string, proxy string) (string, time.Duration, error) {
	authURL := "https://www.googleapis.com/oauth2/v4/token"
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	data.Set("assertion", signedJWT)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return "", 0, fmt.Errorf("new proxy http client failed: %w", err)
	}

	resp, err := client.PostForm(authURL, data)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, err
	}

	accessToken, ok := result["access_token"].(string)
	if !ok {
		return "", 0, fmt.Errorf("failed to get access token: %v", result)
	}
	lifetime := time.Hour
	if seconds, ok := result["expires_in"].(float64); ok && seconds > 0 {
		lifetime = time.Duration(seconds) * time.Second
	}
	return accessToken, lifetime, nil
}
