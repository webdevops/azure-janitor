package janitor

import (
	"testing"
	"time"

	"github.com/webdevops/go-common/utils/to"
	"go.uber.org/zap"

	"github.com/webdevops/azure-janitor/config"
)

func buildJanitorObj() *Janitor {
	opts := config.Opts{}
	opts.Janitor.Tag = "ttl"
	opts.Janitor.TagTarget = "ttl_expiry"
	j := Janitor{Conf: opts}

	return &j
}

func buildTestLogger() *zap.SugaredLogger {
	loggerConfig := zap.NewDevelopmentConfig()
	loggerConfig.OutputPaths = []string{"/dev/null"}
	loggerConfig.ErrorOutputPaths = []string{"/dev/null"}
	logger, _ := loggerConfig.Build()
	return logger.Sugar()
}

func TestDurationParser(t *testing.T) {
	var (
		duration *time.Duration
		err      error
	)
	j := buildJanitorObj()

	duration, err = j.parseExpiryDuration("5m")
	assumeNotError(t, "duration parser", err)
	assumeNotNil(t, "duration", duration)
	assumeDuration(t, "duration", 5*time.Minute, *duration)

	duration, err = j.parseExpiryDuration("5mtest1m")
	assumeError(t, "duration parser", err)
	assumeNil(t, "duration", duration)

	duration, err = j.parseExpiryDuration("PT5M")
	assumeNotError(t, "duration parser", err)
	assumeNotNil(t, "duration", duration)
	assumeDuration(t, "duration", 5*time.Minute, *duration)

	duration, err = j.parseExpiryDuration("P1D")
	assumeNotError(t, "duration parser", err)
	assumeNotNil(t, "duration", duration)
	assumeDuration(t, "duration", 24*time.Hour, *duration)

	duration, err = j.parseExpiryDuration("PT5Mtest")
	assumeError(t, "duration parser", err)
	assumeNil(t, "duration", duration)
}

func TestResourceGroupExpiry(t *testing.T) {
	var resourceExpired, resourceTagRewriteNeeded bool
	var resourceExpireTime *time.Time

	contextLogger := buildTestLogger()

	j := buildJanitorObj()

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

func assumeError(t *testing.T, message string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf(`expected %v w/ error, got: "%v"`, message, err.Error())
	}
}

func assumeNotError(t *testing.T, message string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf(`expected %v w/o error, got: "%v"`, message, err.Error())
	}
}

func assumeNil(t *testing.T, message string, state interface{}) {
	t.Helper()

	switch v := state.(type) {
	case *time.Time:
		if v != nil {
			t.Fatalf(`expected %v state should be <nil>, got: "%v"`, message, v)
		}
	case *time.Duration:
		if v != nil {
			t.Fatalf(`expected %v state should be <nil>, got: "%v"`, message, v)
		}
	default:
		t.Fatalf(`got unexpected type for %v, got: "%v"`, message, v)
	}
}

func assumeNotNil(t *testing.T, message string, state interface{}) {
	t.Helper()

	switch v := state.(type) {
	case *time.Time:
		if v == nil {
			t.Fatalf(`expected %v state should be NOT <nil>, got: "%v"`, message, v)
		}
	case *time.Duration:
		if v == nil {
			t.Fatalf(`expected %v state should be NOT <nil>, got: "%v"`, message, v)
		}
	default:
		t.Fatalf(`got unexpected type for %v, got: "%v"`, message, v)
	}
}

func assumeState(t *testing.T, message string, expectedState, currentState bool) {
	t.Helper()
	if currentState != expectedState {
		t.Fatalf(`expected %v state "%v", got: "%v"`, message, expectedState, currentState)
	}
}

func assumeDuration(t *testing.T, message string, expectedState, currentState time.Duration) {
	t.Helper()
	if currentState != expectedState {
		t.Fatalf(`expected %v state "%v", got: "%v"`, message, expectedState, currentState)
	}
}
