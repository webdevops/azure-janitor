package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/2019-03-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/google/logger"
	"github.com/prometheus/client_golang/prometheus"
	"strings"
	"sync"
	"time"
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

func startAzureJanitor() {
	ctx := context.Background()

	go func() {
		for {
			logger.Infof("Starting run")
			var wgMain sync.WaitGroup
			var wgMetrics sync.WaitGroup

			callbackTtlMetrics := make(chan MetricCollectorList)

			// subscription processing
			for _, subscription := range AzureSubscriptions {
				wgMain.Add(1)
				go func(subscription subscriptions.Subscription) {
					defer wgMain.Done()

					if !opts.JanitorDisableResourceGroups {
						janitorCleanupResourceGroups(ctx, subscription, opts.janitorFilterResourceGroups, callbackTtlMetrics)
					}

					if !opts.JanitorDisableResources {
						janitorCleanupResources(ctx, subscription, opts.janitorFilterResources, callbackTtlMetrics)
					}
				}(subscription)
			}

			// channel collecting gofunc
			wgMetrics.Add(1)
			go func() {
				defer wgMetrics.Done()

				// store metriclists from channel
				ttlMetricListList := []MetricCollectorList{}
				for ttlMetrics := range callbackTtlMetrics {
					ttlMetricListList = append(ttlMetricListList, ttlMetrics)
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

			logger.Infof("Finished run, waiting %s", opts.JanitorInterval.String())
			time.Sleep(opts.JanitorInterval)
		}
	}()
}

func janitorCleanupResources(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- MetricCollectorList) {
	client := resources.NewClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceTtl := MetricCollectorList{}

	resourceResult, err := client.ListComplete(ctx, filter, "", nil)

	if err != nil {
		panic(err)
	}

	for _, resource := range *resourceResult.Response().Value {
		resourceType := *resource.Type

		if resource.Tags != nil {
			resourceExpiryTime, resourceExpired := janitorCheckAzureResourceExpiry(resourceType, *resource.ID, resource.Tags)

			if resourceExpiryTime != nil {
				resourceTtl.AddTime(prometheus.Labels{
					"subscriptionID": *subscription.SubscriptionID,
					"resourceID":     *resource.ID,
					"resourceGroup":  extractResourceGroupFromAzureId(*resource.ID),
					"provider":       extractProviderFromAzureId(*resource.ID),
				}, *resourceExpiryTime)
			}

			if !opts.DryRun && resourceExpired {
				logger.Infof("%s: expired, trying to delete", *resource.ID)
				if _, err := client.DeleteByID(ctx, *resource.ID); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully deleted", *resource.ID)

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *resource.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}

func janitorCleanupResourceGroups(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- MetricCollectorList) {
	client := resources.NewGroupsClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceTtl := MetricCollectorList{}

	resourceGroupResult, err := client.ListComplete(ctx, filter, nil)
	if err != nil {
		panic(err)
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		// resourceGroup.Type is nil
		resourceType := "Microsoft.Resources/resourceGroups"

		if resourceGroup.Tags != nil {
			resourceExpiryTime, resourceExpired := janitorCheckAzureResourceExpiry(resourceType, *resourceGroup.ID, resourceGroup.Tags)

			if resourceExpiryTime != nil {
				resourceTtl.AddTime(prometheus.Labels{
					"subscriptionID": *subscription.SubscriptionID,
					"resourceID":     *resourceGroup.ID,
					"resourceGroup":  *resourceGroup.Name,
					"provider":       resourceType,
				}, *resourceExpiryTime)
			}

			if !opts.DryRun && resourceExpired {
				logger.Infof("%s: expired, trying to delete", *resourceGroup.ID)
				if _, err := client.Delete(ctx, *resourceGroup.Name); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully deleted", *resourceGroup.ID)

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *resourceGroup.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}

func janitorCheckAzureResourceExpiry(resourceType, resourceId string, resourceTags map[string]*string) (resourceExpireTime *time.Time, resourceExpired bool) {
	ttlValue := janitorAzureResourceGetTtlTag(resourceTags)

	if ttlValue != nil {
		if Verbose {
			logger.Infof("%s: checking ttl", resourceId)
		}

		tagValueParsed, tagValueExpired, err := janitorCheckExpiryDate(*ttlValue)

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

func janitorAzureResourceGetTtlTag(tags map[string]*string) (ttlValue *string) {
	for tagName, tagValue := range tags {
		if tagName == opts.JanitorTag && tagValue != nil && *tagValue != "" {
			ttlValue = tagValue
		}
	}

	return
}

func janitorCheckExpiryDate(value string) (parsedTime *time.Time, expired bool, err error) {
	expired = false

	// sanity checks
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	// parse time
	for _, timeFormat := range janitorTimeFormats {
		if val, err := time.Parse(timeFormat, value); err == nil && val.Unix() > 0 {
			parsedTime = &val
			err = nil
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
