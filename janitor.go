package main

import (
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/features"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	tparse "github.com/karrick/tparse/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rickb777/date/period"
	log "github.com/sirupsen/logrus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
	"strings"
	"sync"
	"time"
)

type (
	Janitor struct {
		apiVersionMap map[string]map[string]string
	}
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

func (j *Janitor) Init() {
	j.initAuzreApiVersions()
}

func (j *Janitor) Run() {
	ctx := context.Background()

	go func() {
		for {
			startTime := time.Now()
			log.Infof("start janitor run")
			var wgMain sync.WaitGroup
			var wgMetrics sync.WaitGroup

			callbackTtlMetrics := make(chan *prometheusCommon.MetricList)

			// subscription processing
			for _, subscription := range azureSubscriptions {
				wgMain.Add(1)
				go func(subscription subscriptions.Subscription) {
					defer wgMain.Done()

					if opts.Janitor.EnableResourceGroups {
						j.runResourceGroups(ctx, subscription, opts.Janitor.FilterResourceGroups, callbackTtlMetrics)
					}

					if opts.Janitor.EnableResources {
						j.runResources(ctx, subscription, opts.Janitor.FilterResources, callbackTtlMetrics)
					}

					if opts.Janitor.EnableDeployments {
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

			duration := time.Since(startTime)
			Prometheus.MetricDuration.With(prometheus.Labels{}).Set(duration.Seconds())

			log.WithField("duration", duration.Seconds()).Infof("finished run in %s, waiting %s", duration.String(), opts.Janitor.Interval.String())
			time.Sleep(opts.Janitor.Interval)
		}
	}()
}

func (j *Janitor) initAuzreApiVersions() {
	ctx := context.Background()

	j.apiVersionMap = map[string]map[string]string{}
	for _, subscription := range azureSubscriptions {
		client := features.NewProvidersClient(*subscription.SubscriptionID)
		client.Authorizer = azureAuthorizer

		subscriptionId := *subscription.SubscriptionID

		j.apiVersionMap[subscriptionId] = map[string]string{}

		result, err := client.ListComplete(ctx, nil, "")
		if err != nil {
			panic(err)
		}

		for _, provider := range *result.Response().Value {
			if provider.ResourceTypes == nil {
				continue
			}

			for _, resourceType := range *provider.ResourceTypes {
				if resourceType.APIVersions == nil {
					continue
				}

				resourceTypeName := fmt.Sprintf(
					"%s/%s",
					strings.ToLower(*provider.Namespace),
					strings.ToLower(*resourceType.ResourceType),
				)

				lastApiVersion := ""
				lastApiPreviewVersion := ""
				for _, apiVersion := range *resourceType.APIVersions {
					if strings.Contains(apiVersion, "-preview") {
						if lastApiVersion == "" || lastApiPreviewVersion > apiVersion {
							lastApiPreviewVersion = apiVersion
						}
					} else {
						if lastApiVersion == "" || lastApiVersion > apiVersion {
							lastApiVersion = apiVersion
						}
					}
				}

				if lastApiVersion != "" {
					j.apiVersionMap[subscriptionId][resourceTypeName] = lastApiVersion
				} else if lastApiPreviewVersion != "" {
					j.apiVersionMap[subscriptionId][resourceTypeName] = lastApiPreviewVersion
				}
			}
		}
	}
}

func (j *Janitor) getAzureApiVersionForSubscriptionResourceType(subscriptionId, resourceType string) (apiVersion string) {
	resourceType = strings.ToLower(resourceType)
	if val, ok := j.apiVersionMap[subscriptionId][resourceType]; ok {
		apiVersion = val
	}
	return
}

func (j *Janitor) checkAzureResourceExpiry(logger *log.Entry, resourceType, resourceId string, resourceTags *map[string]*string) (resourceExpireTime *time.Time, resourceExpired bool, resourceTagRewriteNeeded bool) {
	tagName, ttlValue := j.getTtlTagFromAzureResoruce(*resourceTags)

	if ttlValue != nil {
		if opts.Logger.Verbose {
			logger.Infof("checking ttl")
		}

		tagValueParsed, tagValueExpired, timeParseErr := j.checkExpiryDate(*ttlValue)
		if timeParseErr == nil {
			// date parsed successfully
			if tagValueExpired {
				if opts.DryRun {
					logger.Infof("expired, but dryrun active")
				} else {
					resourceExpired = true
				}
			} else {
				if opts.Logger.Verbose {
					logger.Infof("NOT expired")
				}
			}

			resourceExpireTime = tagValueParsed
		} else if val, durationParseErr := j.checkExpiryDuration(*ttlValue); durationParseErr == nil && val != nil {
			// try parse as duration
			logger.Infof("found valid duration (%v)", *ttlValue)
			resourceTagRewriteNeeded = true
			ttlValue := val.Format(time.RFC3339)
			(*resourceTags)[*tagName] = &ttlValue
		} else {
			logger.Errorf("ERROR %s", timeParseErr)
		}
	}

	return
}

func (j *Janitor) getTtlTagFromAzureResoruce(tags map[string]*string) (ttlName, ttlValue *string) {
	for tagName, tagValue := range tags {
		if tagName == opts.Janitor.Tag && tagValue != nil && *tagValue != "" {
			val := tagName
			ttlName = &val
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

	// ISO8601 style duration
	if val, parseErr := period.Parse(value); parseErr == nil {
		// parse duration
		calcTime := time.Now().Add(val.DurationApprox())
		parsedTime = &calcTime
		return
	}

	// golang style duration
	if val, parseErr := tparse.AddDuration(time.Now(), value); parseErr == nil {
		parsedTime = &val
		return
	}

	err = fmt.Errorf("unable to parse '%v' as duration", value)

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
		err = fmt.Errorf("unable to parse time '%s'", value)
	}

	return
}
