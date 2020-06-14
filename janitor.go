package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/google/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rickb777/date/period"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
	"strings"
	"sync"
	"time"
)

type (
	Janitor struct {}
)


var (
	janitorTimeFormats = []string{
		// prefered format
		time.RFC3339,

		// human format
		"2006-01-02 15:04:05 +07:00",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05",

		// allowed formats
		time.RFC822,
		time.RFC822Z,
		time.RFC850,
		time.RFC1123,
		time.RFC1123Z,
		time.RFC3339Nano,
	}
)

func (j *Janitor) Run() {
	ctx := context.Background()

	go func() {
		for {
			startTime := time.Now()
			logger.Infof("Starting run")
			var wgMain sync.WaitGroup
			var wgMetrics sync.WaitGroup

			callbackTtlMetrics := make(chan *prometheusCommon.MetricList)

			// subscription processing
			for _, subscription := range AzureSubscriptions {
				wgMain.Add(1)
				go func(subscription subscriptions.Subscription) {
					defer wgMain.Done()

					if !opts.JanitorDisableResourceGroups {
						j.runResourceGroups(ctx, subscription, opts.janitorFilterResourceGroups, callbackTtlMetrics)
					}

					if !opts.JanitorDisableResources {
						j.runResources(ctx, subscription, opts.janitorFilterResources, callbackTtlMetrics)
					}

					if !opts.JanitorDisableDeployments {
						j.runDeployments(ctx, subscription, callbackTtlMetrics)
					}
				}(subscription)
			}

			// channel collecting gofunc
			wgMetrics.Add(1)
			go func() {
				defer wgMetrics.Done()

				// store metriclists from channel
				ttlMetricListList := []prometheusCommon.MetricList{}
				for ttlMetrics := range callbackTtlMetrics {
					if ttlMetrics != nil {
						ttlMetricListList = append(ttlMetricListList, *ttlMetrics)
					}
				}

				// after channel is closed: reset metric and set them to the new state
				Prometheus.MetricTtlResources.Reset()
				for _, ttlMetrics := range ttlMetricListList {
					ttlMetrics.GaugeSet(Prometheus.MetricTtlResources)
				}
			}()

			// wait for subscription main func, then close channel and wait for metrics
			wgMain.Wait()
			close(callbackTtlMetrics)
			wgMetrics.Wait()

			duration := time.Now().Sub(startTime)
			Prometheus.MetricDuration.With(prometheus.Labels{}).Set(duration.Seconds())

			logger.Infof("Finished run in %s, waiting %s", duration.String(), opts.JanitorInterval.String())
			time.Sleep(opts.JanitorInterval)
		}
	}()
}

func (j *Janitor)  checkAzureResourceExpiry(resourceType, resourceId string, resourceTags *map[string]*string) (resourceExpireTime *time.Time, resourceExpired bool, resourceTagRewriteNeeded bool) {
	tagName, ttlValue := j.getTtlTagFromAzureResoruce(*resourceTags)

	if ttlValue != nil {
		if Verbose {
			logger.Infof("%s: checking ttl", resourceId)
		}

		if val, err := j.checkExpiryDuration(*ttlValue); err == nil && val != nil {
			logger.Infof("%s: found valid duration", resourceId)
			resourceTagRewriteNeeded = true
			ttlValue := val.Format(time.RFC3339)
			(*resourceTags)[*tagName] = &ttlValue
			return
		}

		tagValueParsed, tagValueExpired, err := j.checkExpiryDate(*ttlValue)

		if err == nil {
			if tagValueExpired {
				if opts.DryRun {
					logger.Infof("%s: expired, but dryrun active", resourceId)
				} else {
					resourceExpired = true
				}
			} else {
				if Verbose {
					logger.Infof("%s: NOT expired", resourceId)
				}
			}

			resourceExpireTime = tagValueParsed
		} else {
			logger.Errorf("%s: ERROR %s", resourceId, err)
		}
	}

	return
}

func (j *Janitor) getTtlTagFromAzureResoruce(tags map[string]*string) (ttlName, ttlValue *string) {
	for tagName, tagValue := range tags {
		if tagName == opts.JanitorTag && tagValue != nil && *tagValue != "" {
			ttlName = &tagName
			ttlValue = tagValue
		}
	}

	return
}

func (j *Janitor) checkExpiryDuration(value string) (parsedTime *time.Time, err error) {

	// sanity checks
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	if val, err := period.Parse(value); err == nil {
		// parse duration
		calcTime := time.Now().Add(val.DurationApprox())
		parsedTime = &calcTime
	}

	return
}

func (j *Janitor) checkExpiryDate(value string) (parsedTime *time.Time, expired bool, err error) {
	expired = false

	// sanity checks
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	// parse time
	for _, timeFormat := range janitorTimeFormats {
		if parseVal, parseErr := time.Parse(timeFormat, value); parseErr == nil && parseVal.Unix() > 0 {
			parsedTime = &parseVal
			break
		}
	}

	// check if time could be parsed
	if parsedTime != nil {
		// check if parsed time is before NOW -> expired
		expired = parsedTime.Before(time.Now())
	} else {
		err = errors.New(fmt.Sprintf(
			"Unable to parse time '%s'",
			value,
		))
	}

	return
}
