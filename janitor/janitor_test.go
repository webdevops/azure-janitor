package janitor

import (
	"github.com/Azure/go-autorest/autorest/to"
	log "github.com/sirupsen/logrus"
	"github.com/webdevops/azure-janitor/config"
	"io/ioutil"
	"testing"
	"time"
)

func TestResourceGroupExpiry(t *testing.T) {
	var resourceExpired, resourceTagRewriteNeeded bool
	var resourceExpireTime *time.Time

	logger := log.New()
	logger.Out = ioutil.Discard
	contextLogger := logger.WithField("type", "testing")

	opts := config.Opts{}
	opts.Janitor.Tag = "ttl"
	opts.Janitor.TagTarget = "ttl_expiry"
	j := Janitor{Conf: opts}

	resourceExpireTime, resourceExpired, resourceTagRewriteNeeded = j.checkAzureResourceExpiry(
		contextLogger,
		"resourceGroup",
		"no-ttl-tag",
		&map[string]*string{
			"foobar": to.StringPtr("barfoo"),
		},
	)
	assumeNil(t, "calculated expiry time", resourceExpireTime)
	assumeState(t, `resource expired`, false, resourceExpired)
	assumeState(t, `resource tag rewrite needed`, false, resourceTagRewriteNeeded)

	resourceExpireTime, resourceExpired, resourceTagRewriteNeeded = j.checkAzureResourceExpiry(
		contextLogger,
		"resourceGroup",
		"absolute-time-ttl-tag-already-expired",
		&map[string]*string{
			"foobar": to.StringPtr("barfoo"),
			"ttl":    to.StringPtr(time.Now().Add(-10 * time.Minute).Format(time.RFC3339)),
		},
	)
	assumeNotNil(t, "calculated expiry time", resourceExpireTime)
	assumeState(t, `resource expired`, true, resourceExpired)
	assumeState(t, `resource tag rewrite needed`, false, resourceTagRewriteNeeded)

	resourceExpireTime, resourceExpired, resourceTagRewriteNeeded = j.checkAzureResourceExpiry(
		contextLogger,
		"resourceGroup",
		"absolute-time-ttl-tag-not-expired",
		&map[string]*string{
			"foobar": to.StringPtr("barfoo"),
			"ttl":    to.StringPtr(time.Now().Add(10 * time.Minute).Format(time.RFC3339)),
		},
	)
	assumeNotNil(t, "calculated expiry time", resourceExpireTime)
	assumeState(t, `resource expired`, false, resourceExpired)
	assumeState(t, `resource tag rewrite needed`, false, resourceTagRewriteNeeded)

	resourceExpireTime, resourceExpired, resourceTagRewriteNeeded = j.checkAzureResourceExpiry(
		contextLogger,
		"resourceGroup",
		"relative-time-ttl-tag-not-expired",
		&map[string]*string{
			"foobar": to.StringPtr("barfoo"),
			"ttl":    to.StringPtr("5d"),
		},
	)
	assumeNotNil(t, "calculated expiry time", resourceExpireTime)
	assumeState(t, `resource expired`, false, resourceExpired)
	assumeState(t, `resource tag rewrite needed`, true, resourceTagRewriteNeeded)
}

func assumeNil(t *testing.T, message string, state interface{}) {
	t.Helper()

	switch v := state.(type) {
	case *time.Time:
		if v != nil {
			t.Fatalf(`expected %v state should be <nil>, got: "%v"`, message, v)
		}
	default:
		t.Fatalf(`got unexpected type, got: "%v"`, v)
	}
}

func assumeNotNil(t *testing.T, message string, state interface{}) {
	t.Helper()

	switch v := state.(type) {
	case *time.Time:
		if v == nil {
			t.Fatalf(`expected %v state should be NOT <nil>, got: "%v"`, message, v)
		}
	default:
		t.Fatalf(`got unexpected type, got: "%v"`, v)
	}
}

func assumeState(t *testing.T, message string, expectedState, currentState bool) {
	t.Helper()
	if currentState != expectedState {
		t.Fatalf(`expected %v state "%v", got: "%v"`, message, expectedState, currentState)
	}
}
