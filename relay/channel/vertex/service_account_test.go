package vertex

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "new-router/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubAccessTokenFetch(t *testing.T, fetch func(Credentials, string) (string, time.Duration, error)) {
	t.Helper()
	accessTokens.Clear()
	original := fetchAccessToken
	fetchAccessToken = fetch
	t.Cleanup(func() {
		fetchAccessToken = original
		accessTokens.Clear()
	})
}

func newAccessTokenTestInfo(channelID int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID}}
}

func newAccessTokenTestAdaptor(keyID string) *Adaptor {
	return &Adaptor{AccountCredentials: Credentials{ClientEmail: "sa@project.iam.gserviceaccount.com", PrivateKeyID: keyID}}
}

func TestGetAccessTokenReusesTokenUntilNearExpiry(t *testing.T) {
	var calls atomic.Int32
	stubAccessTokenFetch(t, func(Credentials, string) (string, time.Duration, error) {
		return fmt.Sprintf("token-%d", calls.Add(1)), time.Hour, nil
	})
	info := newAccessTokenTestInfo(7)
	a := newAccessTokenTestAdaptor("key-1")
	first, err := getAccessToken(a, info)
	require.NoError(t, err)
	second, err := getAccessToken(a, info)
	require.NoError(t, err)
	assert.Equal(t, "token-1", first)
	assert.Equal(t, first, second)
	assert.Equal(t, int32(1), calls.Load())
	value, ok := accessTokens.Load("7")
	require.True(t, ok)
	entry := value.(cachedAccessToken)
	entry.expiresAt = time.Now().Add(-time.Second)
	accessTokens.Store("7", entry)
	third, err := getAccessToken(a, info)
	require.NoError(t, err)
	assert.Equal(t, "token-2", third)
	assert.Equal(t, int32(2), calls.Load())
}

func TestGetAccessTokenSeparatesKeySlotsAndReplacedKeys(t *testing.T) {
	var calls atomic.Int32
	stubAccessTokenFetch(t, func(creds Credentials, _ string) (string, time.Duration, error) {
		calls.Add(1)
		return creds.PrivateKeyID, time.Hour, nil
	})
	multiKey := newAccessTokenTestInfo(9)
	multiKey.ChannelIsMultiKey = true
	multiKey.ChannelMultiKeyIndex = 1
	slotOne, err := getAccessToken(newAccessTokenTestAdaptor("slot-1"), multiKey)
	require.NoError(t, err)
	multiKey.ChannelMultiKeyIndex = 2
	slotTwo, err := getAccessToken(newAccessTokenTestAdaptor("slot-2"), multiKey)
	require.NoError(t, err)
	assert.Equal(t, "slot-1", slotOne)
	assert.Equal(t, "slot-2", slotTwo)
	single := newAccessTokenTestInfo(10)
	_, err = getAccessToken(newAccessTokenTestAdaptor("old"), single)
	require.NoError(t, err)
	replaced, err := getAccessToken(newAccessTokenTestAdaptor("new"), single)
	require.NoError(t, err)
	assert.Equal(t, "new", replaced)
	assert.Equal(t, int32(4), calls.Load())
}

func TestGetAccessTokenSharesOneExchangeAcrossConcurrentRequests(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	stubAccessTokenFetch(t, func(Credentials, string) (string, time.Duration, error) {
		calls.Add(1)
		<-release
		return "shared", time.Hour, nil
	})
	info := newAccessTokenTestInfo(11)
	a := newAccessTokenTestAdaptor("key-1")
	const workers = 8
	var wg sync.WaitGroup
	results := make([]string, workers)
	errors := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errors[i] = getAccessToken(a, info)
		}()
	}
	close(release)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
	for i, token := range results {
		require.NoError(t, errors[i])
		assert.Equal(t, "shared", token)
	}
}

func TestGetAccessTokenDoesNotCacheFailures(t *testing.T) {
	var calls atomic.Int32
	stubAccessTokenFetch(t, func(Credentials, string) (string, time.Duration, error) {
		if calls.Add(1) == 1 {
			return "", 0, errors.New("exchange failed")
		}
		return "recovered", time.Hour, nil
	})
	info := newAccessTokenTestInfo(12)
	a := newAccessTokenTestAdaptor("key-1")
	_, err := getAccessToken(a, info)
	require.ErrorContains(t, err, "exchange failed")
	token, err := getAccessToken(a, info)
	require.NoError(t, err)
	assert.Equal(t, "recovered", token)
}
